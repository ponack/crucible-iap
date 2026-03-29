// SPDX-License-Identifier: AGPL-3.0-or-later
// Append-only audit log. Events are INSERT-only; no UPDATE/DELETE allowed.
package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
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
		// Audit failures are logged but never fatal — don't break the request
		// TODO: structured log here
		_ = err
	}
}

// List returns recent audit events for the authenticated org.
func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID")
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, occurred_at, COALESCE(actor_id::text,''), actor_type,
		       action, COALESCE(resource_id,''), COALESCE(resource_type,''),
		       COALESCE(org_id::text,''), context
		FROM audit_events
		WHERE org_id = $1
		ORDER BY occurred_at DESC
		LIMIT 100
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.OccurredAt, &e.ActorID, &e.ActorType,
			&e.Action, &e.ResourceID, &e.ResourceType, &e.OrgID, &e.Context); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		events = append(events, e)
	}
	return c.JSON(http.StatusOK, events)
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
