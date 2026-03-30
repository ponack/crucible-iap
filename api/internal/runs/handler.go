// SPDX-License-Identifier: AGPL-3.0-or-later
package runs

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/pagination"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/worker"
)

type Handler struct {
	pool       *pgxpool.Pool
	queue      *queue.Client
	dispatcher *worker.Dispatcher
	storage    *storage.Client
}

func NewHandler(pool *pgxpool.Pool, q *queue.Client, d *worker.Dispatcher, s *storage.Client) *Handler {
	return &Handler{pool: pool, queue: q, dispatcher: d, storage: s}
}

type Run struct {
	ID         string     `json:"id"`
	StackID    string     `json:"stack_id"`
	Status     string     `json:"status"`
	Type       string     `json:"type"`
	Trigger    string     `json:"trigger"`
	CommitSHA  string     `json:"commit_sha,omitempty"`
	Branch     string     `json:"branch,omitempty"`
	IsDrift    bool       `json:"is_drift"`
	QueuedAt   time.Time  `json:"queued_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

// List returns runs for a specific stack.
func (h *Handler) List(c echo.Context) error {
	stackID := c.Param("stackID")
	p := pagination.Parse(c)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, stack_id, status, type, trigger,
		       COALESCE(commit_sha,''), COALESCE(branch,''),
		       is_drift, queued_at, started_at, finished_at,
		       COUNT(*) OVER () AS total
		FROM runs WHERE stack_id = $1
		ORDER BY queued_at DESC
		LIMIT $2 OFFSET $3
	`, stackID, p.Limit, p.Offset)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []Run
	var total int
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger,
			&r.CommitSHA, &r.Branch, &r.IsDrift, &r.QueuedAt, &r.StartedAt, &r.FinishedAt,
			&total); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, r)
	}
	return c.JSON(http.StatusOK, pagination.Wrap(out, p, total))
}

// Create enqueues a new manual run.
func (h *Handler) Create(c echo.Context) error {
	stackID := c.Param("stackID")
	var req struct {
		Type string `json:"type"` // tracked | proposed | destroy
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Type == "" {
		req.Type = "tracked"
	}

	// Fetch stack details needed to build the job spec
	var stack struct {
		Tool        string
		RunnerImage string
		RepoURL     string
		RepoBranch  string
		ProjectRoot string
	}
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT tool, COALESCE(runner_image,''), repo_url, repo_branch, project_root
		FROM stacks WHERE id = $1
	`, stackID).Scan(&stack.Tool, &stack.RunnerImage, &stack.RepoURL, &stack.RepoBranch, &stack.ProjectRoot)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	var r Run
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO runs (stack_id, type, trigger)
		VALUES ($1, $2, 'manual')
		RETURNING id, stack_id, status, type, trigger, is_drift, queued_at
	`, stackID, req.Type).Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger, &r.IsDrift, &r.QueuedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	apiURL := c.Scheme() + "://" + c.Request().Host
	if _, err := h.queue.EnqueueRun(c.Request().Context(), queue.RunJobArgs{
		RunID:       r.ID,
		StackID:     stackID,
		Tool:        stack.Tool,
		RunnerImage: stack.RunnerImage,
		RepoURL:     stack.RepoURL,
		RepoBranch:  stack.RepoBranch,
		ProjectRoot: stack.ProjectRoot,
		RunType:     req.Type,
		APIURL:      apiURL,
	}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to enqueue run: "+err.Error())
	}

	userID, _ := c.Get("userID").(string)
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "run.created",
		ResourceID:   r.ID,
		ResourceType: "run",
	})

	return c.JSON(http.StatusCreated, r)
}

// Get returns a single run by ID.
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	var r Run
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, stack_id, status, type, trigger,
		       COALESCE(commit_sha,''), COALESCE(branch,''),
		       is_drift, queued_at, started_at, finished_at
		FROM runs WHERE id = $1
	`, id).Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger,
		&r.CommitSHA, &r.Branch, &r.IsDrift, &r.QueuedAt, &r.StartedAt, &r.FinishedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}
	return c.JSON(http.StatusOK, r)
}

// Confirm approves an unconfirmed run and enqueues the apply phase.
func (h *Handler) Confirm(c echo.Context) error {
	id := c.Param("id")
	userID, _ := c.Get("userID").(string)

	var r Run
	err := h.pool.QueryRow(c.Request().Context(), `
		UPDATE runs SET status = 'confirmed', approved_by = $2, approved_at = now()
		WHERE id = $1 AND status = 'unconfirmed'
		RETURNING id, stack_id, type, status
	`, id, userID).Scan(&r.ID, &r.StackID, &r.Type, &r.Status)
	if err != nil {
		return echo.NewHTTPError(http.StatusConflict, "run cannot be confirmed in its current state")
	}

	var stack struct {
		Tool        string
		RunnerImage string
		RepoURL     string
		RepoBranch  string
		ProjectRoot string
	}
	_ = h.pool.QueryRow(c.Request().Context(), `
		SELECT tool, COALESCE(runner_image,''), repo_url, repo_branch, project_root
		FROM stacks WHERE id = $1
	`, r.StackID).Scan(&stack.Tool, &stack.RunnerImage, &stack.RepoURL, &stack.RepoBranch, &stack.ProjectRoot)

	apiURL := c.Scheme() + "://" + c.Request().Host
	_, _ = h.queue.EnqueueRun(c.Request().Context(), queue.RunJobArgs{
		RunID: r.ID, StackID: r.StackID,
		Tool: stack.Tool, RunnerImage: stack.RunnerImage,
		RepoURL: stack.RepoURL, RepoBranch: stack.RepoBranch, ProjectRoot: stack.ProjectRoot,
		RunType: "apply", APIURL: apiURL,
	})

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "run.confirmed", ResourceID: id, ResourceType: "run",
	})
	return c.NoContent(http.StatusNoContent)
}

// Discard rejects an unconfirmed run.
func (h *Handler) Discard(c echo.Context) error {
	id := c.Param("id")
	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET status = 'discarded' WHERE id = $1 AND status = 'unconfirmed'
	`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "run cannot be discarded in its current state")
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: c.Get("userID").(string), Action: "run.discarded", ResourceID: id, ResourceType: "run",
	})
	return c.NoContent(http.StatusNoContent)
}

// Cancel stops an in-progress run.
func (h *Handler) Cancel(c echo.Context) error {
	id := c.Param("id")
	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET status = 'canceled'
		WHERE id = $1 AND status IN ('queued','preparing','planning','unconfirmed','applying')
	`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "run cannot be canceled in its current state")
	}
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: c.Get("userID").(string), Action: "run.canceled", ResourceID: id, ResourceType: "run",
	})
	return c.NoContent(http.StatusNoContent)
}

// Logs streams live run output via Server-Sent Events.
func (h *Handler) Logs(c echo.Context) error {
	id := c.Param("id")

	var status string
	if err := h.pool.QueryRow(c.Request().Context(), `SELECT status FROM runs WHERE id = $1`, id).Scan(&status); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}

	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx/caddy buffering
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming not supported")
	}

	lines, cancel := h.dispatcher.Subscribe(id)
	defer cancel()

	// Confirm connection immediately
	fmt.Fprintf(w, ": connected run=%s\n\n", id)
	flusher.Flush()

	for {
		select {
		case line, open := <-lines:
			if !open {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return nil
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		case <-c.Request().Context().Done():
			return nil
		}
	}
}
