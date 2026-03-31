// SPDX-License-Identifier: AGPL-3.0-or-later
// Internal callbacks called by ephemeral runner containers during a job.
package runs

import (
	"io"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
)

// ReportStatus lets a runner container update the run's intermediate status.
// Only transitions to "planning" and "applying" are permitted here; the worker
// sets all terminal statuses after the container exits.
func (h *Handler) ReportStatus(c echo.Context) error {
	id := c.Param("id")

	// Enforce that the token is scoped to this exact run.
	tokenRunID, _ := c.Get("runID").(string)
	if tokenRunID != id {
		return echo.NewHTTPError(http.StatusForbidden, "token not valid for this run")
	}

	var req struct {
		Status string `json:"status"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	allowed := map[string]bool{"planning": true, "applying": true}
	if !allowed[req.Status] {
		return echo.NewHTTPError(http.StatusBadRequest, "only planning/applying may be reported by runner")
	}

	tag, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET status = $1
		WHERE id = $2 AND status NOT IN ('canceled','failed','finished','discarded')
	`, req.Status, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "run is in a terminal state")
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorType:    "runner",
		Action:       "run.status." + req.Status,
		ResourceID:   id,
		ResourceType: "run",
	})

	return c.NoContent(http.StatusNoContent)
}

// ReportPlanSummary stores the resource change counts reported by the runner
// after running `tofu show -json`. Counts are used for PR comments.
func (h *Handler) ReportPlanSummary(c echo.Context) error {
	id := c.Param("id")

	tokenRunID, _ := c.Get("runID").(string)
	if tokenRunID != id {
		return echo.NewHTTPError(http.StatusForbidden, "token not valid for this run")
	}

	var req struct {
		Add     int `json:"add"`
		Change  int `json:"change"`
		Destroy int `json:"destroy"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET plan_add = $1, plan_change = $2, plan_destroy = $3 WHERE id = $4
	`, req.Add, req.Change, req.Destroy, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	return c.NoContent(http.StatusNoContent)
}

// UploadPlan receives the binary plan artifact from the runner and stores it in MinIO.
// The plan is stored keyed by run ID and the URL is recorded on the run row.
func (h *Handler) UploadPlan(c echo.Context) error {
	id := c.Param("id")

	tokenRunID, _ := c.Get("runID").(string)
	if tokenRunID != id {
		return echo.NewHTTPError(http.StatusForbidden, "token not valid for this run")
	}

	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 512*1024*1024)) // 512 MB cap
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read plan body")
	}

	if err := h.storage.PutPlan(c.Request().Context(), id, body); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to store plan artifact")
	}

	planKey := "plans/" + id + ".tfplan"
	_, _ = h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET plan_url = $1 WHERE id = $2
	`, planKey, id)

	return c.NoContent(http.StatusNoContent)
}
