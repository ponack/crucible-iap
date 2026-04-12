// SPDX-License-Identifier: AGPL-3.0-or-later
package runs

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/pagination"
	"github.com/ponack/crucible-iap/internal/queue"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/worker"
)


type Handler struct {
	pool    *pgxpool.Pool
	cfg     *config.Config
	queue   *queue.Client
	storage *storage.Client
}

func NewHandler(pool *pgxpool.Pool, cfg *config.Config, q *queue.Client, s *storage.Client) *Handler {
	return &Handler{pool: pool, cfg: cfg, queue: q, storage: s}
}

// runnerAPIURL returns the URL runner containers should use to reach the
// Crucible API (state backend + status callbacks). Checks, in order:
//  1. RUNNER_API_URL env var directly (most reliable in Docker deployments)
//  2. cfg.RunnerAPIURL from Viper config (same var, different read path)
//  3. URL derived from the incoming HTTP request (fallback, may be unreachable
//     from inside an isolated Docker network)
func (h *Handler) runnerAPIURL(c echo.Context) string {
	if u := os.Getenv("RUNNER_API_URL"); u != "" {
		return u
	}
	if h.cfg.RunnerAPIURL != "" {
		return h.cfg.RunnerAPIURL
	}
	return c.Scheme() + "://" + c.Request().Host
}

type Run struct {
	ID               string     `json:"id"`
	StackID          string     `json:"stack_id"`
	StackName        string     `json:"stack_name,omitempty"` // populated by ListAll
	Status           string     `json:"status"`
	Type             string     `json:"type"`
	Trigger          string     `json:"trigger"`
	CommitSHA        string     `json:"commit_sha,omitempty"`
	CommitMessage    string     `json:"commit_message,omitempty"`
	Branch           string     `json:"branch,omitempty"`
	IsDrift          bool       `json:"is_drift"`
	PRNumber         *int       `json:"pr_number,omitempty"`
	PRURL            *string    `json:"pr_url,omitempty"`
	PlanAdd          *int       `json:"plan_add,omitempty"`
	PlanChange       *int       `json:"plan_change,omitempty"`
	PlanDestroy      *int       `json:"plan_destroy,omitempty"`
	HasPlan          bool       `json:"has_plan"`
	TriggeredByName  string     `json:"triggered_by_name,omitempty"`
	TriggeredByEmail string     `json:"triggered_by_email,omitempty"`
	ApprovedByName   string     `json:"approved_by_name,omitempty"`
	ApprovedByEmail  string     `json:"approved_by_email,omitempty"`
	ApprovedAt       *time.Time `json:"approved_at,omitempty"`
	QueuedAt         time.Time  `json:"queued_at"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	FinishedAt       *time.Time `json:"finished_at,omitempty"`
}

// ListAll returns paginated runs across all stacks in the authenticated org.
func (h *Handler) ListAll(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	p := pagination.Parse(c)

	conds := []string{"s.org_id = $1"}
	args := []any{orgID}

	if status := c.QueryParam("status"); status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("r.status = $%d", len(args)))
	}
	if typ := c.QueryParam("type"); typ != "" {
		args = append(args, typ)
		conds = append(conds, fmt.Sprintf("r.type = $%d", len(args)))
	}

	where := strings.Join(conds, " AND ")
	args = append(args, p.Limit, p.Offset)
	nLimit, nOffset := len(args)-1, len(args)

	rows, err := h.pool.Query(c.Request().Context(), fmt.Sprintf(`
		SELECT r.id, r.stack_id, s.name,
		       r.status, r.type, r.trigger,
		       COALESCE(r.commit_sha,''), COALESCE(r.branch,''), COALESCE(r.commit_message,''),
		       r.is_drift, r.pr_number, r.pr_url, r.plan_add, r.plan_change, r.plan_destroy,
		       r.queued_at, r.started_at, r.finished_at,
		       COUNT(*) OVER () AS total
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE %s
		ORDER BY r.queued_at DESC
		LIMIT $%d OFFSET $%d
	`, where, nLimit, nOffset), args...)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []Run
	var total int
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.StackID, &r.StackName,
			&r.Status, &r.Type, &r.Trigger,
			&r.CommitSHA, &r.Branch, &r.CommitMessage, &r.IsDrift,
			&r.PRNumber, &r.PRURL, &r.PlanAdd, &r.PlanChange, &r.PlanDestroy,
			&r.QueuedAt, &r.StartedAt, &r.FinishedAt,
			&total); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, r)
	}
	return c.JSON(http.StatusOK, pagination.Wrap(out, p, total))
}

// List returns runs for a specific stack.
func (h *Handler) List(c echo.Context) error {
	stackID := c.Param("stackID")
	p := pagination.Parse(c)

	conds := []string{"stack_id = $1"}
	args := []any{stackID}

	if status := c.QueryParam("status"); status != "" {
		args = append(args, status)
		conds = append(conds, fmt.Sprintf("status = $%d", len(args)))
	}
	if typ := c.QueryParam("type"); typ != "" {
		args = append(args, typ)
		conds = append(conds, fmt.Sprintf("type = $%d", len(args)))
	}

	where := strings.Join(conds, " AND ")
	args = append(args, p.Limit, p.Offset)
	nLimit, nOffset := len(args)-1, len(args)

	rows, err := h.pool.Query(c.Request().Context(), fmt.Sprintf(`
		SELECT id, stack_id, status, type, trigger,
		       COALESCE(commit_sha,''), COALESCE(branch,''), COALESCE(commit_message,''),
		       is_drift, pr_number, pr_url, plan_add, plan_change, plan_destroy,
		       queued_at, started_at, finished_at,
		       COUNT(*) OVER () AS total
		FROM runs
		WHERE %s
		ORDER BY queued_at DESC
		LIMIT $%d OFFSET $%d
	`, where, nLimit, nOffset), args...)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []Run
	var total int
	for rows.Next() {
		var r Run
		if err := rows.Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger,
			&r.CommitSHA, &r.Branch, &r.CommitMessage, &r.IsDrift,
			&r.PRNumber, &r.PRURL, &r.PlanAdd, &r.PlanChange, &r.PlanDestroy,
			&r.QueuedAt, &r.StartedAt, &r.FinishedAt,
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

	apiURL := h.runnerAPIURL(c)
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

// Get returns a single run by ID, scoped to the caller's org.
func (h *Handler) Get(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	var r Run
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT r.id, r.stack_id, r.status, r.type, r.trigger,
		       COALESCE(r.commit_sha,''), COALESCE(r.branch,''), COALESCE(r.commit_message,''),
		       r.is_drift, r.pr_number, r.pr_url, r.plan_add, r.plan_change, r.plan_destroy,
		       r.plan_url IS NOT NULL,
		       COALESCE(tb.name,''), COALESCE(tb.email,''),
		       COALESCE(ab.name,''), COALESCE(ab.email,''),
		       r.approved_at,
		       r.queued_at, r.started_at, r.finished_at
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		LEFT JOIN users tb ON tb.id = r.triggered_by
		LEFT JOIN users ab ON ab.id = r.approved_by
		WHERE r.id = $1 AND s.org_id = $2
	`, id, orgID).Scan(
		&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger,
		&r.CommitSHA, &r.Branch, &r.CommitMessage, &r.IsDrift,
		&r.PRNumber, &r.PRURL, &r.PlanAdd, &r.PlanChange, &r.PlanDestroy,
		&r.HasPlan,
		&r.TriggeredByName, &r.TriggeredByEmail,
		&r.ApprovedByName, &r.ApprovedByEmail,
		&r.ApprovedAt,
		&r.QueuedAt, &r.StartedAt, &r.FinishedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}
	return c.JSON(http.StatusOK, r)
}

// DownloadPlan streams the plan artifact for an unconfirmed or finished run.
func (h *Handler) DownloadPlan(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var hasPlan bool
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT r.plan_url IS NOT NULL
		FROM runs r JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1 AND s.org_id = $2
	`, id, orgID).Scan(&hasPlan)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}
	if !hasPlan {
		return echo.NewHTTPError(http.StatusNotFound, "no plan artifact for this run")
	}

	obj, err := h.storage.GetPlan(c.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "plan artifact not found in storage")
	}
	defer obj.Close()

	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+id[:8]+`.tfplan"`)
	c.Response().Header().Set("Content-Type", "application/octet-stream")
	return c.Stream(http.StatusOK, "application/octet-stream", obj)
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

	apiURL := h.runnerAPIURL(c)
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

// Delete removes a run record and its associated MinIO artifacts.
// Only terminal runs (finished, failed, canceled, discarded) can be deleted.
func (h *Handler) Delete(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID := c.Get("userID").(string)

	// Verify org ownership and terminal status in one query.
	var status string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT r.status FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1 AND s.org_id = $2
	`, id, orgID).Scan(&status)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}

	terminalStates := map[string]bool{
		"finished": true, "failed": true, "canceled": true, "discarded": true,
	}
	if !terminalStates[status] {
		return echo.NewHTTPError(http.StatusConflict, "only terminal runs can be deleted")
	}

	// Best-effort MinIO cleanup before DB delete.
	_ = h.storage.DeleteLog(c.Request().Context(), id)
	_ = h.storage.DeletePlan(c.Request().Context(), id)

	tag, err := h.pool.Exec(c.Request().Context(), `DELETE FROM runs WHERE id = $1`, id)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete run")
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "run.deleted", ResourceID: id, ResourceType: "run",
	})
	return c.NoContent(http.StatusNoContent)
}

// archivedStatuses are run states where the log has been fully written to
// object storage. For these we serve the archived file rather than the
// live broker (which has no data once the worker exits).
var archivedStatuses = map[string]bool{
	"unconfirmed": true, // plan done, awaiting approval
	"finished":    true,
	"failed":      true,
	"canceled":    true,
	"discarded":   true,
}

// Logs serves run output as Server-Sent Events.
// For in-progress runs it tails the live log broker; for completed/archived
// runs it streams the log stored in object storage line by line.
func (h *Handler) Logs(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	var status string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT r.status FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		WHERE r.id = $1 AND s.org_id = $2
	`, id, orgID).Scan(&status)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}

	w := c.Response()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.Writer.(http.Flusher)
	if !ok {
		return echo.NewHTTPError(http.StatusInternalServerError, "streaming not supported")
	}

	fmt.Fprintf(w, ": connected run=%s\n\n", id)
	flusher.Flush()

	// Archived path: stream stored log from object storage.
	if archivedStatuses[status] {
		obj, err := h.storage.GetLog(c.Request().Context(), id)
		if err != nil {
			// Log not found (e.g. run was canceled before any output) — just close.
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}
		defer obj.Close()

		scanner := bufio.NewScanner(obj)
		for scanner.Scan() {
			select {
			case <-c.Request().Context().Done():
				return nil
			default:
			}
			fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
			flusher.Flush()
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return nil
	}

	// Live path: LISTEN for log lines published by the worker via pg_notify.
	// Each run has a dedicated channel; the worker publishes each log line and
	// a final "[DONE]" payload when the job exits.
	conn, err := h.pool.Acquire(c.Request().Context())
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to acquire connection for log streaming")
	}
	defer conn.Release()

	channel := worker.RunLogChannel(id)
	if _, err := conn.Exec(c.Request().Context(), fmt.Sprintf(`LISTEN "%s"`, channel)); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to subscribe to run log")
	}

	for {
		notification, err := conn.Conn().WaitForNotification(c.Request().Context())
		if err != nil {
			// Client disconnected or context cancelled — not an error we surface.
			return nil
		}
		if notification.Payload == "[DONE]" {
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return nil
		}
		fmt.Fprintf(w, "data: %s\n\n", notification.Payload)
		flusher.Flush()
	}
}

// ── Policy results ────────────────────────────────────────────────────────────

// RunPolicyResult is one policy evaluation record attached to a run.
type RunPolicyResult struct {
	ID          string    `json:"id"`
	RunID       string    `json:"run_id"`
	PolicyID    *string   `json:"policy_id,omitempty"`
	PolicyName  string    `json:"policy_name"`
	PolicyType  string    `json:"policy_type"`
	Hook        string    `json:"hook"`
	Allow       bool      `json:"allow"`
	DenyMsgs    []string  `json:"deny_msgs"`
	WarnMsgs    []string  `json:"warn_msgs"`
	TriggerIDs  []string  `json:"trigger_ids"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

// PolicyResults returns all policy evaluation records for a run.
// GET /api/v1/runs/:id/policy-results
func (h *Handler) PolicyResults(c echo.Context) error {
	id := c.Param("id")
	orgID := c.Get("orgID").(string)

	// Verify org ownership.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(), `
		SELECT EXISTS(
			SELECT 1 FROM runs r JOIN stacks s ON s.id = r.stack_id
			WHERE r.id = $1 AND s.org_id = $2
		)
	`, id, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "run not found")
	}

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, run_id, policy_id, policy_name, policy_type, hook,
		       allow, deny_msgs, warn_msgs, trigger_ids, evaluated_at
		FROM run_policy_results
		WHERE run_id = $1
		ORDER BY evaluated_at
	`, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var out []RunPolicyResult
	for rows.Next() {
		var r RunPolicyResult
		if err := rows.Scan(
			&r.ID, &r.RunID, &r.PolicyID, &r.PolicyName, &r.PolicyType, &r.Hook,
			&r.Allow, &r.DenyMsgs, &r.WarnMsgs, &r.TriggerIDs, &r.EvaluatedAt,
		); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, r)
	}
	if out == nil {
		out = []RunPolicyResult{}
	}
	return c.JSON(http.StatusOK, out)
}

// ReportPolicyResults is called by the runner container after evaluating policies.
// POST /api/v1/internal/runs/:id/policy-results
func (h *Handler) ReportPolicyResults(c echo.Context) error {
	id := c.Param("id")

	tokenRunID, _ := c.Get("runID").(string)
	if tokenRunID != id {
		return echo.NewHTTPError(http.StatusForbidden, "token not valid for this run")
	}

	var results []struct {
		PolicyID   *string  `json:"policy_id"`
		PolicyName string   `json:"policy_name"`
		PolicyType string   `json:"policy_type"`
		Hook       string   `json:"hook"`
		Allow      bool     `json:"allow"`
		DenyMsgs   []string `json:"deny_msgs"`
		WarnMsgs   []string `json:"warn_msgs"`
		TriggerIDs []string `json:"trigger_ids"`
	}
	if err := c.Bind(&results); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	for _, r := range results {
		if r.DenyMsgs == nil {
			r.DenyMsgs = []string{}
		}
		if r.WarnMsgs == nil {
			r.WarnMsgs = []string{}
		}
		if r.TriggerIDs == nil {
			r.TriggerIDs = []string{}
		}
		_, err := h.pool.Exec(c.Request().Context(), `
			INSERT INTO run_policy_results
				(run_id, policy_id, policy_name, policy_type, hook, allow, deny_msgs, warn_msgs, trigger_ids)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`, id, r.PolicyID, r.PolicyName, r.PolicyType, r.Hook, r.Allow, r.DenyMsgs, r.WarnMsgs, r.TriggerIDs)
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
	}

	return c.NoContent(http.StatusNoContent)
}

// TriggerDrift creates a manual proposed+drift run for the given stack.
func (h *Handler) TriggerDrift(c echo.Context) error {
	stackID := c.Param("stackID")

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
		INSERT INTO runs (stack_id, type, trigger, is_drift)
		VALUES ($1, 'proposed', 'manual', true)
		RETURNING id, stack_id, status, type, trigger, is_drift, queued_at
	`, stackID).Scan(&r.ID, &r.StackID, &r.Status, &r.Type, &r.Trigger, &r.IsDrift, &r.QueuedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if _, err := h.queue.EnqueueRun(c.Request().Context(), queue.RunJobArgs{
		RunID:       r.ID,
		StackID:     stackID,
		Tool:        stack.Tool,
		RunnerImage: stack.RunnerImage,
		RepoURL:     stack.RepoURL,
		RepoBranch:  stack.RepoBranch,
		ProjectRoot: stack.ProjectRoot,
		RunType:     "proposed",
		APIURL:      h.runnerAPIURL(c),
	}); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to enqueue drift run: "+err.Error())
	}

	userID, _ := c.Get("userID").(string)
	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "run.drift.triggered", ResourceID: r.ID, ResourceType: "run",
	})
	return c.JSON(http.StatusCreated, r)
}
