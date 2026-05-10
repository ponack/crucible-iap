// SPDX-License-Identifier: AGPL-3.0-or-later
// Internal callbacks called by ephemeral runner containers during a job.
package runs

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"path"
	"strings"

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

// ReportCost stores infracost monthly cost deltas reported by the runner
// after running `infracost breakdown --path plan.json --format json`.
func (h *Handler) ReportCost(c echo.Context) error {
	id := c.Param("id")

	tokenRunID, _ := c.Get("runID").(string)
	if tokenRunID != id {
		return echo.NewHTTPError(http.StatusForbidden, "token not valid for this run")
	}

	var req struct {
		MonthlyCostAdd    float64 `json:"monthly_cost_add"`
		MonthlyCostChange float64 `json:"monthly_cost_change"`
		MonthlyCostRemove float64 `json:"monthly_cost_remove"`
		Currency          string  `json:"currency"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	_, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET cost_add = $1, cost_change = $2, cost_remove = $3, cost_currency = $4 WHERE id = $5
	`, req.MonthlyCostAdd, req.MonthlyCostChange, req.MonthlyCostRemove, req.Currency, id)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

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

// planHMAC computes HMAC-SHA256 over plan bytes using the server secret key.
func (h *Handler) planHMAC(data []byte) string {
	mac := hmac.New(sha256.New, []byte(h.cfg.SecretKey))
	mac.Write(data)
	return hex.EncodeToString(mac.Sum(nil))
}

// DownloadPlanInternal is the runner-facing plan download endpoint used by the
// apply phase. Authentication is via the runner JWT (aud=runner, runID claim).
// Unlike the user-facing DownloadPlan, this does not require an org check —
// the run token is already scoped to the exact run being fetched.
// The stored HMAC is verified before streaming the plan to guard against
// plan artifact tampering between the plan and apply phases.
func (h *Handler) DownloadPlanInternal(c echo.Context) error {
	id := c.Param("id")

	tokenRunID, _ := c.Get("runID").(string)
	if tokenRunID != id {
		return echo.NewHTTPError(http.StatusForbidden, "token not valid for this run")
	}

	// Fetch stored HMAC — NULL means a pre-migration run; skip verification.
	var storedHMAC *string
	if err := h.pool.QueryRow(c.Request().Context(), `
		SELECT plan_hmac FROM runs WHERE id = $1
	`, id).Scan(&storedHMAC); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch plan integrity record")
	}

	obj, err := h.storage.GetPlan(c.Request().Context(), id)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "plan artifact not found in storage")
	}
	defer obj.Close()

	data, err := io.ReadAll(obj)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to read plan artifact")
	}

	if storedHMAC != nil {
		if !hmac.Equal([]byte(h.planHMAC(data)), []byte(*storedHMAC)) {
			return echo.NewHTTPError(http.StatusForbidden, "plan artifact integrity check failed")
		}
	}

	c.Response().Header().Set("Content-Disposition", `attachment; filename="plan.tfplan"`)
	return c.Blob(http.StatusOK, "application/octet-stream", data)
}

// parseCacheKey extracts and sanitizes the wildcard *key route param.
func parseCacheKey(c echo.Context) (string, error) {
	key := strings.TrimPrefix(c.Param("key"), "/")
	if key == "" || strings.Contains(key, "..") {
		return "", echo.NewHTTPError(http.StatusBadRequest, "invalid cache key")
	}
	return key, nil
}

// ListProviderCache returns the list of provider keys available in the cache,
// filtered by the optional ?platform= query parameter (e.g. linux_amd64).
func (h *Handler) ListProviderCache(c echo.Context) error {
	platform := c.QueryParam("platform")
	keys, err := h.storage.ListProviderCache(c.Request().Context(), platform)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list provider cache")
	}
	if keys == nil {
		keys = []string{}
	}
	return c.JSON(http.StatusOK, map[string][]string{"keys": keys})
}

// GetProviderCache streams a cached provider binary to the runner.
// The key is the path relative to TF_PLUGIN_CACHE_DIR, e.g.
// registry.terraform.io/hashicorp/aws/5.0.0/linux_amd64/terraform-provider-aws_v5.0.0_x5
func (h *Handler) GetProviderCache(c echo.Context) error {
	key, err := parseCacheKey(c)
	if err != nil {
		return err
	}
	obj, err := h.storage.GetProviderCache(c.Request().Context(), key)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "provider not in cache")
	}
	defer obj.Close()
	filename := path.Base(key)
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.Stream(http.StatusOK, "application/octet-stream", obj)
}

// PutProviderCache stores a provider binary uploaded by the runner.
// The runner checks existence before uploading, so duplicates are rare.
func (h *Handler) PutProviderCache(c echo.Context) error {
	key, err := parseCacheKey(c)
	if err != nil {
		return err
	}
	const maxSize = 600 * 1024 * 1024
	size := c.Request().ContentLength
	if size > maxSize {
		return echo.NewHTTPError(http.StatusRequestEntityTooLarge, "provider binary exceeds 600 MB limit")
	}
	// LimitReader guards against chunked uploads where ContentLength is -1.
	body := io.LimitReader(c.Request().Body, maxSize)
	if err := h.storage.PutProviderCache(c.Request().Context(), key, body, size); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to store provider binary")
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
	if _, err := h.pool.Exec(c.Request().Context(), `
		UPDATE runs SET plan_url = $1, plan_hmac = $2 WHERE id = $3
	`, planKey, h.planHMAC(body), id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to record plan metadata")
	}

	return c.NoContent(http.StatusNoContent)
}

// UploadPlanJSON receives the JSON representation of the plan (tofu show -json)
// and stores it in MinIO for later diffing between runs.
func (h *Handler) UploadPlanJSON(c echo.Context) error {
	id := c.Param("id")

	tokenRunID, _ := c.Get("runID").(string)
	if tokenRunID != id {
		return echo.NewHTTPError(http.StatusForbidden, "token not valid for this run")
	}

	body, err := io.ReadAll(io.LimitReader(c.Request().Body, 64*1024*1024)) // 64 MB cap
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read plan JSON body")
	}

	if err := h.storage.PutPlanJSON(c.Request().Context(), id, body); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to store plan JSON")
	}

	return c.NoContent(http.StatusNoContent)
}
