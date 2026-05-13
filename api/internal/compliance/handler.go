// SPDX-License-Identifier: AGPL-3.0-or-later
package compliance

import (
	"context"
	_ "embed"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/policy"
)

// Rego source files embedded at compile time.
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

const currentVersion = "1.0.0"

type packDef struct {
	Slug        string
	Name        string
	Description string
	Policies    []policyDef
}

type policyDef struct {
	Name        string
	Description string
	Type        policy.Type
	Body        string
}

var catalog = []packDef{
	{
		Slug:        "soc2",
		Name:        "SOC 2",
		Description: "SOC 2 Type II controls for security, availability, and confidentiality (CC6, CC7).",
		Policies: []policyDef{
			{Name: "SOC2: Approval Required", Description: "CC6.1 — tracked runs require approval before apply.", Type: policy.TypePostPlan, Body: string(regoSOC2Approval)},
			{Name: "SOC2: Unencrypted Storage", Description: "CC6.7 — S3 and RDS must have encryption enabled.", Type: policy.TypePostPlan, Body: string(regoSOC2Storage)},
			{Name: "SOC2: Logging Required", Description: "CC7.2 — CloudTrail must have logging enabled.", Type: policy.TypePostPlan, Body: string(regoSOC2Logging)},
		},
	},
	{
		Slug:        "cis-aws",
		Name:        "CIS AWS Foundations",
		Description: "CIS Amazon Web Services Foundations Benchmark v2.0 IAM and storage controls.",
		Policies: []policyDef{
			{Name: "CIS AWS: No Root Access Keys", Description: "CIS 1.4 — root account must not have access keys.", Type: policy.TypePostPlan, Body: string(regoCISRootKeys)},
			{Name: "CIS AWS: S3 Block Public Access", Description: "CIS 2.1.5 — S3 buckets must block public access.", Type: policy.TypePostPlan, Body: string(regoCISS3Public)},
			{Name: "CIS AWS: IAM Password Policy", Description: "CIS 1.8 — IAM password policy minimum length >= 14.", Type: policy.TypePostPlan, Body: string(regoCISPassword)},
		},
	},
	{
		Slug:        "hipaa",
		Name:        "HIPAA",
		Description: "HIPAA Security Rule safeguards for electronic protected health information (ePHI).",
		Policies: []policyDef{
			{Name: "HIPAA: PHI Encryption at Rest", Description: "§164.312(a)(2)(iv) — RDS, DynamoDB, and EFS must be encrypted.", Type: policy.TypePostPlan, Body: string(regoHIPAAEncrypt)},
			{Name: "HIPAA: Audit Logging", Description: "§164.312(b) — CloudTrail log file validation must be enabled.", Type: policy.TypePostPlan, Body: string(regoHIPAALogging)},
		},
	},
	{
		Slug:        "pci-dss",
		Name:        "PCI-DSS",
		Description: "PCI Data Security Standard v4.0 network and encryption controls.",
		Policies: []policyDef{
			{Name: "PCI-DSS: No Public Ingress", Description: "Req 1.3 — security groups must not allow unrestricted inbound on sensitive ports.", Type: policy.TypePostPlan, Body: string(regoPCIIngress)},
			{Name: "PCI-DSS: TLS Enforcement", Description: "Req 4.2 — ALB listeners and RDS must enforce TLS.", Type: policy.TypePostPlan, Body: string(regoPCITLS)},
		},
	},
}

type Handler struct {
	pool   *pgxpool.Pool
	engine *policy.Engine
}

func NewHandler(pool *pgxpool.Pool, engine *policy.Engine) *Handler {
	return &Handler{pool: pool, engine: engine}
}

type packResponse struct {
	ID            string     `json:"id"`
	Slug          string     `json:"slug"`
	Name          string     `json:"name"`
	Description   string     `json:"description"`
	Version       string     `json:"version"`
	InstalledAt   time.Time  `json:"installed_at"`
	LastSyncedAt  *time.Time `json:"last_synced_at"`
	PolicyCount   int        `json:"policy_count"`
}

type catalogEntry struct {
	Slug        string        `json:"slug"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	PolicyCount int           `json:"policy_count"`
	Installed   *packResponse `json:"installed,omitempty"`
}

// GetCatalog returns the 4 available pack definitions with installed status for the org.
func (h *Handler) GetCatalog(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, slug, name, version, installed_at, last_synced_at,
		       (SELECT count(*) FROM policies WHERE pack_id = policy_packs.id) AS policy_count
		FROM policy_packs WHERE org_id = $1
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	installed := map[string]*packResponse{}
	for rows.Next() {
		var p packResponse
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Version, &p.InstalledAt, &p.LastSyncedAt, &p.PolicyCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		installed[p.Slug] = &p
	}

	entries := make([]catalogEntry, 0, len(catalog))
	for _, def := range catalog {
		e := catalogEntry{
			Slug:        def.Slug,
			Name:        def.Name,
			Description: def.Description,
			PolicyCount: len(def.Policies),
			Installed:   installed[def.Slug],
		}
		entries = append(entries, e)
	}
	return c.JSON(http.StatusOK, entries)
}

// Install creates a policy_packs row and upserts all its policies into the policies table.
func (h *Handler) Install(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var req struct {
		Slug string `json:"slug"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	def := findDef(req.Slug)
	if def == nil {
		return echo.NewHTTPError(http.StatusNotFound, "unknown pack slug")
	}

	ctx := c.Request().Context()

	var packID string
	err := h.pool.QueryRow(ctx, `
		INSERT INTO policy_packs (org_id, slug, name, version, last_synced_at)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (org_id, slug) DO UPDATE SET version = EXCLUDED.version, last_synced_at = now()
		RETURNING id
	`, orgID, def.Slug, def.Name, currentVersion).Scan(&packID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	if err := upsertPackPolicies(ctx, h.pool, h.engine, orgID, packID, def); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	var p packResponse
	err = h.pool.QueryRow(ctx, `
		SELECT id, slug, name, version, installed_at, last_synced_at,
		       (SELECT count(*) FROM policies WHERE pack_id = $1)
		FROM policy_packs WHERE id = $1
	`, packID).Scan(&p.ID, &p.Slug, &p.Name, &p.Version, &p.InstalledAt, &p.LastSyncedAt, &p.PolicyCount)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.installed", ResourceID: packID, ResourceType: "policy_pack",
	})
	return c.JSON(http.StatusCreated, p)
}

// Sync re-embeds the latest policy bodies for an installed pack.
func (h *Handler) Sync(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	packID := c.Param("id")

	var slug string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT slug FROM policy_packs WHERE id = $1 AND org_id = $2
	`, packID, orgID).Scan(&slug)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "pack not found")
	}

	def := findDef(slug)
	if def == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "pack definition not found")
	}

	ctx := c.Request().Context()

	if err := upsertPackPolicies(ctx, h.pool, h.engine, orgID, packID, def); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	_, err = h.pool.Exec(ctx, `
		UPDATE policy_packs SET version = $1, last_synced_at = now() WHERE id = $2
	`, currentVersion, packID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.synced", ResourceID: packID, ResourceType: "policy_pack",
	})
	return c.NoContent(http.StatusNoContent)
}

// Uninstall removes a pack and all its policies (cascade).
func (h *Handler) Uninstall(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)
	packID := c.Param("id")

	ctx := c.Request().Context()

	// Collect policy IDs before deletion so we can unload them from the engine.
	rows, err := h.pool.Query(ctx, `SELECT id FROM policies WHERE pack_id = $1`, packID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	var policyIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			policyIDs = append(policyIDs, id)
		}
	}
	rows.Close()

	_, err = h.pool.Exec(ctx, `DELETE FROM policy_packs WHERE id = $1 AND org_id = $2`, packID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	for _, id := range policyIDs {
		h.engine.Unload(id)
	}

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.uninstalled", ResourceID: packID, ResourceType: "policy_pack",
	})
	return c.NoContent(http.StatusNoContent)
}

// ListStackPacks returns packs attached to a stack.
func (h *Handler) ListStackPacks(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	stackID := c.Param("id")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT pp.id, pp.slug, pp.name, pp.version, pp.installed_at, pp.last_synced_at,
		       (SELECT count(*) FROM policies WHERE pack_id = pp.id)
		FROM stack_policy_packs spp
		JOIN policy_packs pp ON pp.id = spp.pack_id
		WHERE spp.stack_id = $1 AND pp.org_id = $2
		ORDER BY pp.name
	`, stackID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	out := []packResponse{}
	for rows.Next() {
		var p packResponse
		if err := rows.Scan(&p.ID, &p.Slug, &p.Name, &p.Version, &p.InstalledAt, &p.LastSyncedAt, &p.PolicyCount); err != nil {
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

	// Verify the pack belongs to this org.
	var exists bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM policy_packs WHERE id = $1 AND org_id = $2)
	`, req.PackID, orgID).Scan(&exists)
	if err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "pack not found")
	}

	_, err = h.pool.Exec(ctx, `
		INSERT INTO stack_policy_packs (stack_id, pack_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, stackID, req.PackID)
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
	packID := c.Param("packID")

	ctx := c.Request().Context()

	_, err := h.pool.Exec(ctx, `
		DELETE FROM stack_policy_packs spp
		USING policy_packs pp
		WHERE spp.stack_id = $1 AND spp.pack_id = $2 AND pp.id = spp.pack_id AND pp.org_id = $3
	`, stackID, packID, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(ctx, h.pool, audit.Event{
		ActorID: userID, Action: "compliance_pack.detached", ResourceID: stackID, ResourceType: "stack",
	})
	return c.NoContent(http.StatusNoContent)
}

func findDef(slug string) *packDef {
	for i := range catalog {
		if catalog[i].Slug == slug {
			return &catalog[i]
		}
	}
	return nil
}

func upsertPackPolicies(ctx context.Context, pool *pgxpool.Pool, engine *policy.Engine, orgID, packID string, def *packDef) error {
	for _, pd := range def.Policies {
		var policyID string
		err := pool.QueryRow(ctx, `
			INSERT INTO policies (org_id, name, description, type, body, is_active, pack_id)
			VALUES ($1, $2, $3, $4, $5, true, $6)
			ON CONFLICT (pack_id, name) WHERE pack_id IS NOT NULL DO UPDATE
				SET body = EXCLUDED.body, description = EXCLUDED.description,
				    is_active = true, updated_at = now()
			RETURNING id
		`, orgID, pd.Name, pd.Description, string(pd.Type), pd.Body, packID).Scan(&policyID)
		if err != nil {
			return err
		}
		_ = engine.Load(ctx, policyID, pd.Name, pd.Type, pd.Body) //nolint:errcheck
	}
	return nil
}
