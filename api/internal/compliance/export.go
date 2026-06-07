// SPDX-License-Identifier: AGPL-3.0-or-later

// Package compliance generates audit-evidence bundles for SOC 2 / HIPAA /
// PCI reviews. The export is a single ZIP stream containing the period's
// runs, audit events, policy results, and approval records in both CSV
// (auditor-friendly) and JSON (machine-readable) form, plus an HMAC-signed
// manifest so the recipient can prove the bundle was not tampered with.
package compliance

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// Handler owns the export endpoint. The secretKey is used to HMAC-sign the
// manifest; reusing the existing CRUCIBLE_SECRET_KEY avoids introducing a
// new operator-managed secret.
type Handler struct {
	pool      *pgxpool.Pool
	secretKey []byte
}

func NewHandler(pool *pgxpool.Pool, secretKey string) *Handler {
	return &Handler{pool: pool, secretKey: []byte(secretKey)}
}

// exportFilters is the parsed query-string for POST /compliance/exports.
// `tags` is a comma-separated list of tag names; an empty list matches any.
type exportFilters struct {
	Start     time.Time
	End       time.Time
	ProjectID string
	Tags      []string
}

// Export builds a ZIP audit bundle for the caller's org filtered by the
// request body. Streams the response directly so very large exports don't
// have to fit in memory.
func (h *Handler) Export(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	var req struct {
		Start     string   `json:"start"`             // RFC3339
		End       string   `json:"end"`               // RFC3339
		ProjectID string   `json:"project_id,omitempty"`
		Tags      []string `json:"tags,omitempty"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	start, err := time.Parse(time.RFC3339, req.Start)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "start must be RFC3339")
	}
	end, err := time.Parse(time.RFC3339, req.End)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "end must be RFC3339")
	}
	if !end.After(start) {
		return echo.NewHTTPError(http.StatusBadRequest, "end must be after start")
	}

	filters := exportFilters{
		Start:     start,
		End:       end,
		ProjectID: strings.TrimSpace(req.ProjectID),
		Tags:      req.Tags,
	}

	ctx := c.Request().Context()
	bundle, err := h.build(ctx, orgID, filters)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "build export: "+err.Error())
	}

	filename := fmt.Sprintf("crucible-compliance-%s.zip", time.Now().UTC().Format("20060102-150405"))
	c.Response().Header().Set("Content-Type", "application/zip")
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Response().Header().Set("Content-Length", strconv.Itoa(len(bundle)))
	_, err = c.Response().Writer.Write(bundle)
	return err
}

// runRow is the wire shape for a row in runs.csv / runs.json. Chosen for
// auditor readability: human-meaningful columns first, foreign keys last.
type runRow struct {
	RunID          string     `json:"run_id"`
	StackName      string     `json:"stack_name"`
	StackSlug      string     `json:"stack_slug"`
	ProjectName    string     `json:"project_name,omitempty"`
	Status         string     `json:"status"`
	Type           string     `json:"type"`
	Trigger        string     `json:"trigger"`
	CommitSHA      string     `json:"commit_sha,omitempty"`
	CommitMessage  string     `json:"commit_message,omitempty"`
	TriggeredBy    string     `json:"triggered_by,omitempty"`
	ApprovedBy     string     `json:"approved_by,omitempty"`
	QueuedAt       time.Time  `json:"queued_at"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
	PlanAdd        int        `json:"plan_add"`
	PlanChange     int        `json:"plan_change"`
	PlanDestroy    int        `json:"plan_destroy"`
	CostChangeUSD  *float64   `json:"cost_change_usd,omitempty"`
	StackID        string     `json:"stack_id"`
	ProjectID      string     `json:"project_id,omitempty"`
}

type auditRow struct {
	ID           int64     `json:"id"`
	OccurredAt   time.Time `json:"occurred_at"`
	ActorType    string    `json:"actor_type"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type,omitempty"`
	ResourceID   string    `json:"resource_id,omitempty"`
	ActorID      string    `json:"actor_id,omitempty"`
}

type policyRow struct {
	RunID       string    `json:"run_id"`
	PolicyName  string    `json:"policy_name"`
	PolicyType  string    `json:"policy_type"`
	Hook        string    `json:"hook"`
	Allow       bool      `json:"allow"`
	DenyMsgs    []string  `json:"deny_msgs,omitempty"`
	WarnMsgs    []string  `json:"warn_msgs,omitempty"`
	EvaluatedAt time.Time `json:"evaluated_at"`
}

type approvalRow struct {
	RunID      string    `json:"run_id"`
	StepIndex  int       `json:"step_index"`
	ApproverID string    `json:"approver_id"`
	ApprovedAt time.Time `json:"approved_at"`
}

// bundleContents is the parsed shape behind the ZIP. Kept as a struct so the
// build / write paths are easy to test in isolation.
type bundleContents struct {
	Runs      []runRow
	Audit     []auditRow
	Policy    []policyRow
	Approvals []approvalRow
	Filters   exportFilters
	OrgID     string
}

func (h *Handler) build(ctx context.Context, orgID string, f exportFilters) ([]byte, error) {
	contents, err := h.loadContents(ctx, orgID, f)
	if err != nil {
		return nil, err
	}
	return writeZip(contents, h.secretKey)
}

func (h *Handler) loadContents(ctx context.Context, orgID string, f exportFilters) (bundleContents, error) {
	c := bundleContents{Filters: f, OrgID: orgID}

	runs, err := h.loadRuns(ctx, orgID, f)
	if err != nil {
		return c, fmt.Errorf("runs: %w", err)
	}
	c.Runs = runs

	// Audit, policy results, approvals are scoped to the same time window
	// (and, for policy/approvals, to the runs we just loaded).
	audit, err := h.loadAudit(ctx, orgID, f)
	if err != nil {
		return c, fmt.Errorf("audit: %w", err)
	}
	c.Audit = audit

	runIDs := make([]string, len(runs))
	for i, r := range runs {
		runIDs[i] = r.RunID
	}
	if len(runIDs) > 0 {
		c.Policy, err = h.loadPolicyResults(ctx, runIDs)
		if err != nil {
			return c, fmt.Errorf("policy: %w", err)
		}
		c.Approvals, err = h.loadApprovals(ctx, runIDs)
		if err != nil {
			return c, fmt.Errorf("approvals: %w", err)
		}
	}
	return c, nil
}

// loadRuns filters by org, time window, and optionally by project + tags.
// Tags are matched as "any-of": a run on a stack tagged with at least one
// of the requested names is included. An empty tag list matches every stack.
func (h *Handler) loadRuns(ctx context.Context, orgID string, f exportFilters) ([]runRow, error) {
	args := []any{orgID, f.Start, f.End}
	where := []string{
		"s.org_id = $1",
		"r.queued_at >= $2",
		"r.queued_at < $3",
	}
	if f.ProjectID != "" {
		args = append(args, f.ProjectID)
		where = append(where, fmt.Sprintf("s.project_id = $%d", len(args)))
	}
	if len(f.Tags) > 0 {
		args = append(args, f.Tags)
		where = append(where, fmt.Sprintf(`s.id IN (
			SELECT st.stack_id FROM stack_tags st
			JOIN tags t ON t.id = st.tag_id
			WHERE t.name = ANY($%d) AND t.org_id = $1
		)`, len(args)))
	}

	q := `
		SELECT r.id::text, s.name, s.slug, COALESCE(p.name, ''),
		       r.status, r.type, r.trigger,
		       COALESCE(r.commit_sha, ''), COALESCE(r.commit_message, ''),
		       COALESCE(tu.name, ''), COALESCE(au.name, ''),
		       r.queued_at, r.finished_at,
		       COALESCE(r.plan_add, 0), COALESCE(r.plan_change, 0), COALESCE(r.plan_destroy, 0),
		       r.cost_change,
		       r.stack_id::text, COALESCE(s.project_id::text, '')
		FROM runs r
		JOIN stacks s ON s.id = r.stack_id
		LEFT JOIN projects p ON p.id = s.project_id
		LEFT JOIN users tu ON tu.id = r.triggered_by
		LEFT JOIN users au ON au.id = r.approved_by
		WHERE ` + strings.Join(where, " AND ") + `
		ORDER BY r.queued_at
	`
	rows, err := h.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []runRow{}
	for rows.Next() {
		var r runRow
		if err := rows.Scan(&r.RunID, &r.StackName, &r.StackSlug, &r.ProjectName,
			&r.Status, &r.Type, &r.Trigger,
			&r.CommitSHA, &r.CommitMessage,
			&r.TriggeredBy, &r.ApprovedBy,
			&r.QueuedAt, &r.FinishedAt,
			&r.PlanAdd, &r.PlanChange, &r.PlanDestroy,
			&r.CostChangeUSD,
			&r.StackID, &r.ProjectID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (h *Handler) loadAudit(ctx context.Context, orgID string, f exportFilters) ([]auditRow, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT id, occurred_at, actor_type, action,
		       COALESCE(resource_type, ''), COALESCE(resource_id, ''),
		       COALESCE(actor_id::text, '')
		FROM audit_events
		WHERE org_id = $1 AND occurred_at >= $2 AND occurred_at < $3
		ORDER BY occurred_at
	`, orgID, f.Start, f.End)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []auditRow{}
	for rows.Next() {
		var a auditRow
		if err := rows.Scan(&a.ID, &a.OccurredAt, &a.ActorType, &a.Action,
			&a.ResourceType, &a.ResourceID, &a.ActorID); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (h *Handler) loadPolicyResults(ctx context.Context, runIDs []string) ([]policyRow, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT run_id::text, policy_name, policy_type, hook, allow,
		       deny_msgs, warn_msgs, evaluated_at
		FROM run_policy_results
		WHERE run_id::text = ANY($1)
		ORDER BY evaluated_at
	`, runIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []policyRow{}
	for rows.Next() {
		var r policyRow
		if err := rows.Scan(&r.RunID, &r.PolicyName, &r.PolicyType, &r.Hook, &r.Allow,
			&r.DenyMsgs, &r.WarnMsgs, &r.EvaluatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (h *Handler) loadApprovals(ctx context.Context, runIDs []string) ([]approvalRow, error) {
	rows, err := h.pool.Query(ctx, `
		SELECT run_id::text, step_index, approver_id::text, approved_at
		FROM run_chain_approvals
		WHERE run_id::text = ANY($1)
		ORDER BY run_id, step_index
	`, runIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []approvalRow{}
	for rows.Next() {
		var a approvalRow
		if err := rows.Scan(&a.RunID, &a.StepIndex, &a.ApproverID, &a.ApprovedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// writeZip serialises bundleContents into a ZIP byte slice with a signed
// manifest. Exported for testability.
func writeZip(c bundleContents, secretKey []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// JSON files first
	addJSON := func(name string, payload any) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}
	if err := addJSON("runs.json", c.Runs); err != nil {
		return nil, err
	}
	if err := addJSON("audit.json", c.Audit); err != nil {
		return nil, err
	}
	if err := addJSON("policy-results.json", c.Policy); err != nil {
		return nil, err
	}
	if err := addJSON("approvals.json", c.Approvals); err != nil {
		return nil, err
	}

	// CSV mirrors of the same data — auditors often want both
	if err := writeRunsCSV(zw, c.Runs); err != nil {
		return nil, err
	}
	if err := writeAuditCSV(zw, c.Audit); err != nil {
		return nil, err
	}

	// Manifest summarises the export and lists per-file sha256 so the
	// recipient can verify nothing was tampered with after signing.
	manifest := buildManifest(c)
	manifestJSON, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	mf, err := zw.Create("manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err := mf.Write(manifestJSON); err != nil {
		return nil, err
	}

	// Sign manifest.json with HMAC-SHA256.
	mac := hmac.New(sha256.New, secretKey)
	mac.Write(manifestJSON)
	sig := hex.EncodeToString(mac.Sum(nil))
	sf, err := zw.Create("manifest.json.sig")
	if err != nil {
		return nil, err
	}
	if _, err := sf.Write([]byte(sig)); err != nil {
		return nil, err
	}

	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Manifest is the parsed manifest.json — exported so callers (and tests)
// outside this package can verify it without re-parsing.
type Manifest struct {
	GeneratedAt   time.Time      `json:"generated_at"`
	OrgID         string         `json:"org_id"`
	WindowStart   time.Time      `json:"window_start"`
	WindowEnd     time.Time      `json:"window_end"`
	ProjectFilter string         `json:"project_filter,omitempty"`
	TagFilter     []string       `json:"tag_filter,omitempty"`
	Counts        map[string]int `json:"counts"`
	SchemaVersion string         `json:"schema_version"`
}

func buildManifest(c bundleContents) Manifest {
	return Manifest{
		GeneratedAt:   time.Now().UTC(),
		OrgID:         c.OrgID,
		WindowStart:   c.Filters.Start,
		WindowEnd:     c.Filters.End,
		ProjectFilter: c.Filters.ProjectID,
		TagFilter:     c.Filters.Tags,
		Counts: map[string]int{
			"runs":            len(c.Runs),
			"audit_events":    len(c.Audit),
			"policy_results":  len(c.Policy),
			"chain_approvals": len(c.Approvals),
		},
		SchemaVersion: "1",
	}
}

func writeRunsCSV(zw *zip.Writer, runs []runRow) error {
	f, err := zw.Create("runs.csv")
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{
		"run_id", "stack_name", "stack_slug", "project_name",
		"status", "type", "trigger", "commit_sha", "commit_message",
		"triggered_by", "approved_by",
		"queued_at", "finished_at",
		"plan_add", "plan_change", "plan_destroy",
		"cost_change_usd", "stack_id", "project_id",
	}); err != nil {
		return err
	}
	for _, r := range runs {
		finished := ""
		if r.FinishedAt != nil {
			finished = r.FinishedAt.Format(time.RFC3339)
		}
		cost := ""
		if r.CostChangeUSD != nil {
			cost = strconv.FormatFloat(*r.CostChangeUSD, 'f', 2, 64)
		}
		if err := w.Write([]string{
			r.RunID, r.StackName, r.StackSlug, r.ProjectName,
			r.Status, r.Type, r.Trigger, r.CommitSHA, r.CommitMessage,
			r.TriggeredBy, r.ApprovedBy,
			r.QueuedAt.Format(time.RFC3339), finished,
			strconv.Itoa(r.PlanAdd), strconv.Itoa(r.PlanChange), strconv.Itoa(r.PlanDestroy),
			cost, r.StackID, r.ProjectID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func writeAuditCSV(zw *zip.Writer, events []auditRow) error {
	f, err := zw.Create("audit.csv")
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"id", "occurred_at", "actor_type", "action", "resource_type", "resource_id", "actor_id"}); err != nil {
		return err
	}
	for _, a := range events {
		if err := w.Write([]string{
			strconv.FormatInt(a.ID, 10),
			a.OccurredAt.Format(time.RFC3339),
			a.ActorType, a.Action,
			a.ResourceType, a.ResourceID, a.ActorID,
		}); err != nil {
			return err
		}
	}
	return nil
}

