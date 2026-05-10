// SPDX-License-Identifier: AGPL-3.0-or-later
package runs

import (
	"encoding/json"
	"net/http"

	"github.com/labstack/echo/v4"
)

// PlanDiff is the response from GET /api/v1/stacks/:id/plan-diff.
type PlanDiff struct {
	FromRunID string            `json:"from_run_id"`
	ToRunID   string            `json:"to_run_id"`
	New       []PlanDiffEntry   `json:"new"`       // resources that appear in "to" but not "from"
	Removed   []PlanDiffEntry   `json:"removed"`   // resources that appear in "from" but not "to"
	Changed   []PlanDiffChanged `json:"changed"`   // resources in both but with different actions or attributes
}

// PlanDiffEntry is a resource that was added or removed between two plans.
type PlanDiffEntry struct {
	Address string   `json:"address"`
	Type    string   `json:"type"`
	Actions []string `json:"actions"`
}

// PlanDiffChanged is a resource whose planned changes differ between two plans.
type PlanDiffChanged struct {
	Address    string   `json:"address"`
	Type       string   `json:"type"`
	FromActions []string `json:"from_actions"`
	ToActions   []string `json:"to_actions"`
	// AttrsBefore/AttrsAfter are populated when the actions are identical but
	// the planned attribute values differ.
	AttrsBefore map[string]any `json:"attrs_before,omitempty"`
	AttrsAfter  map[string]any `json:"attrs_after,omitempty"`
}

// tfPlanJSON is the subset of tofu/terraform show -json we care about.
type tfPlanJSON struct {
	ResourceChanges []tfResourceChange `json:"resource_changes"`
}

type tfResourceChange struct {
	Address string   `json:"address"`
	Type    string   `json:"type"`
	Change  tfChange `json:"change"`
}

type tfChange struct {
	Actions []string `json:"actions"`
	Before  any      `json:"before"`
	After   any      `json:"after"`
}

// GetPlanDiff returns a structured diff between two plan runs on the same stack.
// GET /api/v1/stacks/:id/plan-diff?from=<runID>&to=<runID>
func (h *Handler) GetPlanDiff(c echo.Context) error {
	stackID := c.Param("id")
	fromID := c.QueryParam("from")
	toID := c.QueryParam("to")

	if fromID == "" || toID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "from and to query params are required")
	}
	if fromID == toID {
		return echo.NewHTTPError(http.StatusBadRequest, "from and to must be different runs")
	}

	ctx := c.Request().Context()

	// Verify both runs belong to this stack.
	var fromStack, toStack string
	_ = h.pool.QueryRow(ctx, `SELECT stack_id FROM runs WHERE id = $1`, fromID).Scan(&fromStack)
	_ = h.pool.QueryRow(ctx, `SELECT stack_id FROM runs WHERE id = $1`, toID).Scan(&toStack)
	if fromStack != stackID || toStack != stackID {
		return echo.NewHTTPError(http.StatusNotFound, "one or both runs not found on this stack")
	}

	fromBytes, err := h.storage.GetPlanJSON(ctx, fromID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "plan JSON not available for 'from' run — only runs created after version pinning is enabled have stored plan JSON")
	}
	toBytes, err := h.storage.GetPlanJSON(ctx, toID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "plan JSON not available for 'to' run")
	}

	var fromPlan, toPlan tfPlanJSON
	if err := json.Unmarshal(fromBytes, &fromPlan); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to parse from-plan JSON")
	}
	if err := json.Unmarshal(toBytes, &toPlan); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to parse to-plan JSON")
	}

	diff := buildPlanDiff(fromID, toID, fromPlan.ResourceChanges, toPlan.ResourceChanges)
	return c.JSON(http.StatusOK, diff)
}

func buildPlanDiff(fromID, toID string, from, to []tfResourceChange) PlanDiff {
	fromMap := indexPlanResources(from)
	toMap := indexPlanResources(to)

	result := PlanDiff{
		FromRunID: fromID,
		ToRunID:   toID,
		New:       []PlanDiffEntry{},
		Removed:   []PlanDiffEntry{},
		Changed:   []PlanDiffChanged{},
	}

	for addr, r := range toMap {
		if _, ok := fromMap[addr]; !ok {
			result.New = append(result.New, PlanDiffEntry{
				Address: addr,
				Type:    r.Type,
				Actions: r.Change.Actions,
			})
		}
	}

	for addr, r := range fromMap {
		if _, ok := toMap[addr]; !ok {
			result.Removed = append(result.Removed, PlanDiffEntry{
				Address: addr,
				Type:    r.Type,
				Actions: r.Change.Actions,
			})
		}
	}

	for addr, tr := range toMap {
		fr, ok := fromMap[addr]
		if !ok {
			continue
		}
		changed := planResourceChanged(fr, tr)
		if changed != nil {
			result.Changed = append(result.Changed, *changed)
		}
	}

	return result
}

func indexPlanResources(changes []tfResourceChange) map[string]tfResourceChange {
	m := make(map[string]tfResourceChange, len(changes))
	for _, r := range changes {
		if !isNoOp(r.Change.Actions) {
			m[r.Address] = r
		}
	}
	return m
}

func isNoOp(actions []string) bool {
	return len(actions) == 1 && actions[0] == "no-op"
}

func actionsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func planResourceChanged(fr, tr tfResourceChange) *PlanDiffChanged {
	if !actionsEqual(fr.Change.Actions, tr.Change.Actions) {
		return &PlanDiffChanged{
			Address:     tr.Address,
			Type:        tr.Type,
			FromActions: fr.Change.Actions,
			ToActions:   tr.Change.Actions,
		}
	}

	before, after, chg := diffPlanAttrs(fr.Change.After, tr.Change.After)
	if !chg {
		return nil
	}
	return &PlanDiffChanged{
		Address:     tr.Address,
		Type:        tr.Type,
		FromActions: tr.Change.Actions,
		ToActions:   tr.Change.Actions,
		AttrsBefore: before,
		AttrsAfter:  after,
	}
}

// diffPlanAttrs extracts keys where planned "after" values differ between two plans.
// Values are capped at 512 chars if strings to keep payloads small.
func diffPlanAttrs(fromAfter, toAfter any) (before, after map[string]any, changed bool) {
	before = map[string]any{}
	after = map[string]any{}

	fromMap, ok1 := fromAfter.(map[string]any)
	toMap, ok2 := toAfter.(map[string]any)
	if !ok1 || !ok2 {
		return before, after, false
	}

	allKeys := make(map[string]struct{}, len(fromMap)+len(toMap))
	for k := range fromMap {
		allKeys[k] = struct{}{}
	}
	for k := range toMap {
		allKeys[k] = struct{}{}
	}

	for k := range allKeys {
		fv := capString(fromMap[k])
		tv := capString(toMap[k])
		fj, _ := json.Marshal(fv)
		tj, _ := json.Marshal(tv)
		if string(fj) != string(tj) {
			before[k] = fv
			after[k] = tv
			changed = true
		}
	}
	return before, after, changed
}

func capString(v any) any {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if len(s) > 512 {
		return s[:512] + "…"
	}
	return s
}
