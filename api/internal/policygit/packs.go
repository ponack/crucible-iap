// SPDX-License-Identifier: AGPL-3.0-or-later
package policygit

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/policies"
	"github.com/ponack/crucible-iap/internal/policy"
	"github.com/ponack/crucible-iap/internal/queue"
)

const (
	packRepoURL = "https://github.com/ponack/crucible-policies"
	packVersion = "1.0.0"
)

// Rego source files embedded at compile time — seeded on install for offline-safe first use.
var (
	//go:embed rego/soc2/approval_required.rego
	regoSOC2Approval []byte
	//go:embed rego/soc2/unencrypted_storage.rego
	regoSOC2Storage []byte
	//go:embed rego/soc2/logging_required.rego
	regoSOC2Logging []byte

	//go:embed rego/cis-aws/iam_root_no_access_keys.rego
	regoCISRootKeys []byte
	//go:embed rego/cis-aws/s3_block_public_access.rego
	regoCISS3Public []byte
	//go:embed rego/cis-aws/iam_password_policy.rego
	regoCISPassword []byte

	//go:embed rego/hipaa/phi_encryption.rego
	regoHIPAAEncrypt []byte
	//go:embed rego/hipaa/audit_logging.rego
	regoHIPAALogging []byte

	//go:embed rego/pci-dss/no_public_ingress.rego
	regoPCIIngress []byte
	//go:embed rego/pci-dss/tls_enforcement.rego
	regoPCITLS []byte
)

type packSeed struct {
	Name string
	Type policy.Type
	Path string // relative path matching the file in ponack/crucible-policies
	Body string
}

type packDef struct {
	Slug        string
	Name        string
	Description string
	RepoPath    string // subdirectory in ponack/crucible-policies
	Seeds       []packSeed
}

var packCatalog = []packDef{
	{
		Slug:        "soc2",
		Name:        "SOC 2",
		Description: "SOC 2 Type II controls for security, availability, and confidentiality (CC6, CC7).",
		RepoPath:    "soc2",
		Seeds: []packSeed{
			{Name: "SOC2: Approval Required", Type: policy.TypePostPlan, Path: "approval_required.rego", Body: string(regoSOC2Approval)},
			{Name: "SOC2: Unencrypted Storage", Type: policy.TypePostPlan, Path: "unencrypted_storage.rego", Body: string(regoSOC2Storage)},
			{Name: "SOC2: Logging Required", Type: policy.TypePostPlan, Path: "logging_required.rego", Body: string(regoSOC2Logging)},
		},
	},
	{
		Slug:        "cis-aws",
		Name:        "CIS AWS Foundations",
		Description: "CIS Amazon Web Services Foundations Benchmark v2.0 IAM and storage controls.",
		RepoPath:    "cis-aws",
		Seeds: []packSeed{
			{Name: "CIS AWS: No Root Access Keys", Type: policy.TypePostPlan, Path: "iam_root_no_access_keys.rego", Body: string(regoCISRootKeys)},
			{Name: "CIS AWS: S3 Block Public Access", Type: policy.TypePostPlan, Path: "s3_block_public_access.rego", Body: string(regoCISS3Public)},
			{Name: "CIS AWS: IAM Password Policy", Type: policy.TypePostPlan, Path: "iam_password_policy.rego", Body: string(regoCISPassword)},
		},
	},
	{
		Slug:        "hipaa",
		Name:        "HIPAA",
		Description: "HIPAA Security Rule safeguards for electronic protected health information (ePHI).",
		RepoPath:    "hipaa",
		Seeds: []packSeed{
			{Name: "HIPAA: PHI Encryption at Rest", Type: policy.TypePostPlan, Path: "phi_encryption.rego", Body: string(regoHIPAAEncrypt)},
			{Name: "HIPAA: Audit Logging", Type: policy.TypePostPlan, Path: "audit_logging.rego", Body: string(regoHIPAALogging)},
		},
	},
	{
		Slug:        "pci-dss",
		Name:        "PCI-DSS",
		Description: "PCI Data Security Standard v4.0 network and encryption controls.",
		RepoPath:    "pci-dss",
		Seeds: []packSeed{
			{Name: "PCI-DSS: No Public Ingress", Type: policy.TypePostPlan, Path: "no_public_ingress.rego", Body: string(regoPCIIngress)},
			{Name: "PCI-DSS: TLS Enforcement", Type: policy.TypePostPlan, Path: "tls_enforcement.rego", Body: string(regoPCITLS)},
		},
	},
}

// ── Response types ────────────────────────────────────────────────────────────

type PackSource struct {
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	LastSyncedAt  *time.Time `json:"last_synced_at"`
	LastSyncSHA   string     `json:"last_sync_sha"`
	LastSyncError string     `json:"last_sync_error,omitempty"`
	PolicyCount   int        `json:"policy_count"`
}

type CatalogEntry struct {
	Slug        string      `json:"slug"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	PolicyCount int         `json:"policy_count"`
	Installed   *PackSource `json:"installed,omitempty"`
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GetCatalog lists the 4 available compliance packs with installed status for the org.
func (h *Handler) GetCatalog(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, pack_slug, name, last_synced_at, last_sync_sha, last_sync_error,
		       (SELECT count(*) FROM policies WHERE git_source_id = policy_git_sources.id)
		FROM policy_git_sources
		WHERE org_id = $1 AND pack_slug IS NOT NULL
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	installed := map[string]*PackSource{}
	for rows.Next() {
		var p PackSource
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.LastSyncedAt, &p.LastSyncSHA, &p.LastSyncError, &p.PolicyCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		installed[p.Slug] = &p
	}

	entries := make([]CatalogEntry, 0, len(packCatalog))
	for _, def := range packCatalog {
		entries = append(entries, CatalogEntry{
			Slug:        def.Slug,
			Name:        def.Name,
			Description: def.Description,
			PolicyCount: len(def.Seeds),
			Installed:   installed[def.Slug],
		})
	}
	return c.JSON(http.StatusOK, entries)
}

// InstallPack creates a policy_git_sources row for a compliance pack and seeds
// the embedded Rego policies so the pack works immediately without network access.
func (h *Handler) InstallPack(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var req struct {
		Slug string `json:"slug"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	def := findPackDef(req.Slug)
	if def == nil {
		return echo.NewHTTPError(http.StatusNotFound, "unknown pack slug")
	}

	ctx := c.Request().Context()

	var sourceID string
	err := h.pool.QueryRow(ctx, `
		INSERT INTO policy_git_sources
		  (org_id, name, repo_url, branch, path, vcs_provider, pack_slug,
		   webhook_secret, last_sync_sha, last_synced_at, created_by)
		VALUES ($1,$2,$3,'main',$4,'github',$5,'','embedded-`+packVersion+`',now(),$6)
		ON CONFLICT (org_id, pack_slug) WHERE pack_slug IS NOT NULL
		DO UPDATE SET last_synced_at = now()
		RETURNING id
	`, orgID, def.Name, packRepoURL, def.RepoPath, def.Slug, userID).Scan(&sourceID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := seedPackPolicies(ctx, h.pool, h.engine, orgID, sourceID, def); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var p PackSource
	err = h.pool.QueryRow(ctx, `
		SELECT id, pack_slug, name, last_synced_at, last_sync_sha, last_sync_error,
		       (SELECT count(*) FROM policies WHERE git_source_id = $1)
		FROM policy_git_sources WHERE id = $1
	`, sourceID).Scan(&p.ID, &p.Slug, &p.Name, &p.LastSyncedAt, &p.LastSyncSHA, &p.LastSyncError, &p.PolicyCount)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.installed", ResourceID: sourceID, ResourceType: "policy_git_source",
	})
	return c.JSON(http.StatusCreated, p)
}

// SyncPack re-seeds embedded Rego and enqueues a live git sync to pull latest from GitHub.
func (h *Handler) SyncPack(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	sourceID := c.Param("id")

	var slug string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT pack_slug FROM policy_git_sources
		WHERE id=$1 AND org_id=$2 AND pack_slug IS NOT NULL
	`, sourceID, orgID).Scan(&slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "pack not found")
	}

	def := findPackDef(slug)
	if def == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "pack definition missing")
	}

	ctx := c.Request().Context()

	if err := seedPackPolicies(ctx, h.pool, h.engine, orgID, sourceID, def); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	_ = h.queue.EnqueuePolicySync(ctx, queue.PolicySyncArgs{SourceID: sourceID})

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.synced", ResourceID: sourceID, ResourceType: "policy_git_source",
	})
	return c.NoContent(http.StatusNoContent)
}

// UninstallPack deletes the git source row — cascades to policies and stack_policy_sources.
func (h *Handler) UninstallPack(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	sourceID := c.Param("id")

	ctx := c.Request().Context()

	policyRows, err := h.pool.Query(ctx, `SELECT id FROM policies WHERE git_source_id=$1`, sourceID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	var policyIDs []string
	for policyRows.Next() {
		var id string
		if err := policyRows.Scan(&id); err == nil {
			policyIDs = append(policyIDs, id)
		}
	}
	policyRows.Close()

	tag, err := h.pool.Exec(ctx,
		`DELETE FROM policy_git_sources WHERE id=$1 AND org_id=$2 AND pack_slug IS NOT NULL`,
		sourceID, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "pack not found")
	}

	for _, id := range policyIDs {
		h.engine.Unload(id)
	}

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.uninstalled", ResourceID: sourceID, ResourceType: "policy_git_source",
	})
	return c.NoContent(http.StatusNoContent)
}

// ListStackPacks returns compliance packs (pack_slug IS NOT NULL) attached to a stack.
func (h *Handler) ListStackPacks(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	stackID := c.Param("id")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT pgs.id, pgs.pack_slug, pgs.name, pgs.last_synced_at, pgs.last_sync_sha,
		       pgs.last_sync_error,
		       (SELECT count(*) FROM policies WHERE git_source_id = pgs.id)
		FROM stack_policy_sources sps
		JOIN policy_git_sources pgs ON pgs.id = sps.git_source_id
		WHERE sps.stack_id=$1 AND pgs.org_id=$2 AND pgs.pack_slug IS NOT NULL
		ORDER BY pgs.name
	`, stackID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	out := []PackSource{}
	for rows.Next() {
		var p PackSource
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.LastSyncedAt, &p.LastSyncSHA, &p.LastSyncError, &p.PolicyCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		out = append(out, p)
	}
	return c.JSON(http.StatusOK, out)
}

// AttachPack links a compliance pack to a stack (idempotent).
func (h *Handler) AttachPack(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	stackID := c.Param("id")

	var req struct {
		PackID string `json:"pack_id"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	ctx := c.Request().Context()

	var exists bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM policy_git_sources WHERE id=$1 AND org_id=$2 AND pack_slug IS NOT NULL
		)
	`, req.PackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "pack not found")
	}

	_, err := h.pool.Exec(ctx,
		`INSERT INTO stack_policy_sources (stack_id, git_source_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`,
		stackID, req.PackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.attached", ResourceID: stackID, ResourceType: "stack",
	})
	return c.NoContent(http.StatusNoContent)
}

// DetachPack removes a compliance pack from a stack.
func (h *Handler) DetachPack(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	stackID := c.Param("id")
	sourceID := c.Param("packID")

	_, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM stack_policy_sources sps
		USING policy_git_sources pgs
		WHERE sps.stack_id=$1 AND sps.git_source_id=$2
		  AND pgs.id = sps.git_source_id AND pgs.org_id=$3
	`, stackID, sourceID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.detached", ResourceID: stackID, ResourceType: "stack",
	})
	return c.NoContent(http.StatusNoContent)
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func findPackDef(slug string) *packDef {
	for i := range packCatalog {
		if packCatalog[i].Slug == slug {
			return &packCatalog[i]
		}
	}
	return nil
}

// seedPackPolicies upserts embedded Rego into the policies table and reloads the engine.
// Each policy is keyed on (git_source_id, git_source_path) matching the worker's upsert logic.
func seedPackPolicies(ctx context.Context, pool *pgxpool.Pool, engine *policy.Engine, orgID, sourceID string, def *packDef) error {
	for _, seed := range def.Seeds {
		var policyID string
		err := pool.QueryRow(ctx, `
			INSERT INTO policies (org_id, name, type, body, git_source_id, git_source_path, is_active)
			VALUES ($1,$2,$3,$4,$5,$6,true)
			ON CONFLICT (git_source_id, git_source_path)
				WHERE git_source_id IS NOT NULL AND git_source_path <> ''
			DO UPDATE SET body=EXCLUDED.body, name=EXCLUDED.name, updated_at=now()
			RETURNING id
		`, orgID, seed.Name, string(seed.Type), seed.Body, sourceID, seed.Path).Scan(&policyID)
		if err != nil {
			return err
		}
		_ = engine.Load(ctx, policyID, seed.Name, seed.Type, seed.Body)
	}
	return policies.LoadEngine(ctx, pool, engine)
}
