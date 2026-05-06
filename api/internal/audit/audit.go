// SPDX-License-Identifier: AGPL-3.0-or-later
// Append-only audit log. Events are INSERT-only; no UPDATE/DELETE allowed.
package audit

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/pagination"
)

type Event struct {
	ID           int64           `json:"id"`
	OccurredAt   time.Time       `json:"occurred_at"`
	ActorID      string          `json:"actor_id,omitempty"`
	ActorType    string          `json:"actor_type"`
	Action       string          `json:"action"`
	ResourceID   string          `json:"resource_id,omitempty"`
	ResourceType string          `json:"resource_type,omitempty"`
	OrgID        string          `json:"org_id,omitempty"`
	IPAddress    string          `json:"ip_address,omitempty"`
	Context      json.RawMessage `json:"context,omitempty"`
}

type Handler struct{ pool *pgxpool.Pool }

func NewHandler(pool *pgxpool.Pool) *Handler { return &Handler{pool: pool} }

// Record appends an audit event. Call before returning API responses.
func Record(ctx context.Context, pool *pgxpool.Pool, e Event) {
	if e.ActorType == "" {
		e.ActorType = "user"
	}
	if e.Context == nil {
		e.Context = json.RawMessage("{}")
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO audit_events
		  (actor_id, actor_type, action, resource_id, resource_type, org_id, ip_address, context)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, nilIfEmpty(e.ActorID), e.ActorType, e.Action,
		nilIfEmpty(e.ResourceID), nilIfEmpty(e.ResourceType),
		nilIfEmpty(e.OrgID), parseIP(e.IPAddress), e.Context)
	if err != nil {
		// Audit failures are logged but never fatal — don't break the request.
		slog.Error("audit: failed to record event", "action", e.Action, "err", err)
	}
}

// List returns audit events for the authenticated org, newest first.
// Supports optional query params: ?action=, ?resource_type=, ?actor_id=
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID")
	p := pagination.Parse(c)

	conds := []string{"org_id = $1"}
	args := []any{orgID}

	if action := c.QueryParam("action"); action != "" {
		args = append(args, action+"%")
		conds = append(conds, fmt.Sprintf("action LIKE $%d", len(args)))
	}
	if rt := c.QueryParam("resource_type"); rt != "" {
		args = append(args, rt)
		conds = append(conds, fmt.Sprintf("resource_type = $%d", len(args)))
	}
	if actor := c.QueryParam("actor_id"); actor != "" {
		args = append(args, actor)
		conds = append(conds, fmt.Sprintf("actor_id::text = $%d", len(args)))
	}

	where := strings.Join(conds, " AND ")
	args = append(args, p.Limit, p.Offset)
	nLimit, nOffset := len(args)-1, len(args)

	rows, err := h.pool.Query(c.Request().Context(), fmt.Sprintf(`
		SELECT id, occurred_at, COALESCE(actor_id::text,''), actor_type,
		       action, COALESCE(resource_id,''), COALESCE(resource_type,''),
		       COALESCE(org_id::text,''), context,
		       COUNT(*) OVER () AS total
		FROM audit_events
		WHERE %s
		ORDER BY occurred_at DESC
		LIMIT $%d OFFSET $%d
	`, where, nLimit, nOffset), args...)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var events []Event
	var total int
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorID, &e.ActorType,
			&e.Action, &e.ResourceID, &e.ResourceType, &e.OrgID, &e.Context,
			&total); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		events = append(events, e)
	}
	return c.JSON(http.StatusOK, pagination.Wrap(events, p, total))
}

// Export streams all matching audit events as a CSV or JSON file download.
// Supports ?format=csv (default) or ?format=json, plus the same filters as List.
func (h *Handler) Export(c echo.Context) error {
	orgID := c.Get("orgID")
	format := c.QueryParam("format")
	if format != "json" {
		format = "csv"
	}

	conds := []string{"org_id = $1"}
	args := []any{orgID}

	if action := c.QueryParam("action"); action != "" {
		args = append(args, action+"%")
		conds = append(conds, fmt.Sprintf("action LIKE $%d", len(args)))
	}
	if rt := c.QueryParam("resource_type"); rt != "" {
		args = append(args, rt)
		conds = append(conds, fmt.Sprintf("resource_type = $%d", len(args)))
	}
	if actor := c.QueryParam("actor_id"); actor != "" {
		args = append(args, actor)
		conds = append(conds, fmt.Sprintf("actor_id::text = $%d", len(args)))
	}

	where := strings.Join(conds, " AND ")
	rows, err := h.pool.Query(c.Request().Context(), fmt.Sprintf(`
		SELECT id, occurred_at, COALESCE(actor_id::text,''), actor_type,
		       action, COALESCE(resource_id,''), COALESCE(resource_type,''), context
		FROM audit_events
		WHERE %s
		ORDER BY occurred_at DESC
	`, where), args...)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	if format == "json" {
		c.Response().Header().Set("Content-Disposition", `attachment; filename="audit-export.json"`)
		c.Response().Header().Set("Content-Type", "application/json; charset=utf-8")
		c.Response().WriteHeader(http.StatusOK)
		enc := json.NewEncoder(c.Response())
		_, _ = c.Response().Write([]byte("[\n"))
		first := true
		for rows.Next() {
			var e Event
			if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorID, &e.ActorType,
				&e.Action, &e.ResourceID, &e.ResourceType, &e.Context); err != nil {
				break
			}
			if !first {
				_, _ = c.Response().Write([]byte(",\n"))
			}
			first = false
			_ = enc.Encode(e)
		}
		_, _ = c.Response().Write([]byte("]\n"))
		return nil
	}

	c.Response().Header().Set("Content-Disposition", `attachment; filename="audit-export.csv"`)
	c.Response().Header().Set("Content-Type", "text/csv; charset=utf-8")
	c.Response().WriteHeader(http.StatusOK)

	w := csv.NewWriter(c.Response())
	_ = w.Write([]string{"id", "occurred_at", "actor_id", "actor_type", "action", "resource_id", "resource_type", "context"})

	for rows.Next() {
		var id int64
		var occurredAt time.Time
		var actorID, actorType, action, resourceID, resourceType string
		var ctx json.RawMessage
		if err := rows.Scan(&id, &occurredAt, &actorID, &actorType, &action, &resourceID, &resourceType, &ctx); err != nil {
			break
		}
		_ = w.Write([]string{
			fmt.Sprintf("%d", id),
			occurredAt.UTC().Format(time.RFC3339),
			actorID, actorType, action, resourceID, resourceType,
			string(ctx),
		})
	}
	w.Flush()
	return nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func parseIP(s string) any {
	if s == "" {
		return nil
	}
	addr, err := netip.ParseAddr(s)
	if err != nil {
		return nil
	}
	return addr.String()
}

// ── Partition maintenance ─────────────────────────────────────────────────────

// EnsurePartitions creates monthly audit_events partitions for the current
// month and the next [ahead] months. Safe to call repeatedly — uses
// CREATE TABLE IF NOT EXISTS so existing partitions are left untouched.
func EnsurePartitions(ctx context.Context, pool *pgxpool.Pool, ahead int) error {
	now := time.Now().UTC()
	for i := 0; i <= ahead; i++ {
		target := now.AddDate(0, i, 0)
		y, m, _ := target.Date()
		tableName := fmt.Sprintf("audit_events_%04d_%02d", y, int(m))
		from := fmt.Sprintf("%04d-%02d-01", y, int(m))
		next := time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC)
		to := fmt.Sprintf("%04d-%02d-01", next.Year(), int(next.Month()))

		_, err := pool.Exec(ctx, fmt.Sprintf(
			`CREATE TABLE IF NOT EXISTS %s PARTITION OF audit_events`+
				` FOR VALUES FROM ('%s') TO ('%s')`,
			tableName, from, to,
		))
		if err != nil {
			return fmt.Errorf("create partition %s: %w", tableName, err)
		}
	}
	return nil
}

// StartPartitionMaintainer calls EnsurePartitions immediately, then again at
// the start of each calendar month, always staying [ahead] months ahead.
// Must be called after the database pool is ready.
func StartPartitionMaintainer(ctx context.Context, pool *pgxpool.Pool) {
	const ahead = 2

	if err := EnsurePartitions(ctx, pool, ahead); err != nil {
		slog.Error("audit: partition maintenance failed at startup", "err", err)
	} else {
		slog.Info("audit: partitions ensured", "months_ahead", ahead)
	}

	go func() {
		for {
			// Sleep until the 2nd of next month (UTC) to avoid midnight edge cases.
			now := time.Now().UTC()
			next := time.Date(now.Year(), now.Month()+1, 2, 0, 0, 0, 0, time.UTC)
			timer := time.NewTimer(next.Sub(now))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
				if err := EnsurePartitions(ctx, pool, ahead); err != nil {
					slog.Error("audit: monthly partition maintenance failed", "err", err)
				} else {
					slog.Info("audit: monthly partition maintenance complete", "months_ahead", ahead)
				}
			}
		}
	}()
}
