// SPDX-License-Identifier: AGPL-3.0-or-later
// Package registry implements the Terraform Module Registry Protocol v1 and
// the management API for publishing and yanking private modules.
package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/storage"
)

var (
	reName      = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{0,63}$`)
	reVersion   = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+`)
)

type Handler struct {
	pool    *pgxpool.Pool
	storage *storage.Client
	cfg     *config.Config
}

func NewHandler(pool *pgxpool.Pool, store *storage.Client, cfg *config.Config) *Handler {
	return &Handler{pool: pool, storage: store, cfg: cfg}
}

// ── Response types ────────────────────────────────────────────────────────────

type Module struct {
	ID            string    `json:"id"`
	Namespace     string    `json:"namespace"`
	Name          string    `json:"name"`
	Provider      string    `json:"provider"`
	Version       string    `json:"version"`
	Readme        string    `json:"readme,omitempty"`
	Yanked        bool      `json:"yanked"`
	PublishedBy   string    `json:"published_by,omitempty"`
	PublishedAt   time.Time `json:"published_at"`
	DownloadCount int64     `json:"download_count"`
}

// ── Management API ────────────────────────────────────────────────────────────

func (h *Handler) List(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	q := c.QueryParam("q")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT m.id, m.namespace, m.name, m.provider, m.version,
		       m.readme, m.yanked, COALESCE(u.email,'') AS published_by,
		       m.published_at, m.download_count
		FROM registry_modules m
		LEFT JOIN users u ON u.id = m.published_by
		WHERE m.org_id = $1
		  AND ($2 = '' OR m.name ILIKE '%' || $2 || '%'
		       OR m.namespace ILIKE '%' || $2 || '%'
		       OR m.provider ILIKE '%' || $2 || '%')
		ORDER BY m.namespace, m.name, m.provider, m.version DESC
	`, orgID, q)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list modules")
	}
	defer rows.Close()

	mods := []Module{}
	for rows.Next() {
		var m Module
		if err := rows.Scan(&m.ID, &m.Namespace, &m.Name, &m.Provider, &m.Version,
			&m.Readme, &m.Yanked, &m.PublishedBy, &m.PublishedAt, &m.DownloadCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		mods = append(mods, m)
	}
	return c.JSON(http.StatusOK, mods)
}

func (h *Handler) Get(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	var m Module
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT m.id, m.namespace, m.name, m.provider, m.version,
		       m.readme, m.yanked, COALESCE(u.email,'') AS published_by,
		       m.published_at, m.download_count
		FROM registry_modules m
		LEFT JOIN users u ON u.id = m.published_by
		WHERE m.id = $1 AND m.org_id = $2
	`, id, orgID).Scan(&m.ID, &m.Namespace, &m.Name, &m.Provider, &m.Version,
		&m.Readme, &m.Yanked, &m.PublishedBy, &m.PublishedAt, &m.DownloadCount)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "module not found")
	}
	return c.JSON(http.StatusOK, m)
}

func (h *Handler) Publish(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	namespace := c.FormValue("namespace")
	name := c.FormValue("name")
	provider := c.FormValue("provider")
	version := c.FormValue("version")

	if err := validateModuleFields(namespace, name, provider, version); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	file, _, err := c.Request().FormFile("module")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "module file is required")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 256<<20))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read module file")
	}
	readme := extractReadme(data)

	key := storage.ModuleKey(namespace, name, provider, version)
	if err := h.storage.PutModule(c.Request().Context(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to store module")
	}

	var m Module
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO registry_modules
		  (org_id, namespace, name, provider, version, storage_key, readme, published_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (org_id, namespace, name, provider, version)
		DO UPDATE SET storage_key=$6, readme=$7, published_by=$8, published_at=NOW(), yanked=FALSE
		RETURNING id, namespace, name, provider, version, readme, yanked, published_at, download_count
	`, orgID, namespace, name, provider, version, key, readme, userID).Scan(
		&m.ID, &m.Namespace, &m.Name, &m.Provider, &m.Version,
		&m.Readme, &m.Yanked, &m.PublishedAt, &m.DownloadCount)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to record module")
	}
	m.PublishedBy = userID
	return c.JSON(http.StatusCreated, m)
}

func (h *Handler) Yank(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE registry_modules SET yanked=TRUE WHERE id=$1 AND org_id=$2`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "module not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Terraform Module Registry Protocol v1 ────────────────────────────────────

// Versions implements GET /registry/v1/modules/:namespace/:name/:provider/versions
func (h *Handler) Versions(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns, name, provider := c.Param("namespace"), c.Param("name"), c.Param("provider")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT version FROM registry_modules
		WHERE org_id=$1 AND namespace=$2 AND name=$3 AND provider=$4 AND NOT yanked
		ORDER BY published_at DESC
	`, orgID, ns, name, provider)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "query failed")
	}
	defer rows.Close()

	type versionEntry struct {
		Version string `json:"version"`
	}
	versions := []versionEntry{}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		versions = append(versions, versionEntry{Version: v})
	}

	if len(versions) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no versions found")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"modules": []map[string]any{{
			"source":   fmt.Sprintf("%s/%s/%s", ns, name, provider),
			"versions": versions,
		}},
	})
}

// GetVersion implements GET /registry/v1/modules/:namespace/:name/:provider/:version
func (h *Handler) GetVersion(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns, name, provider, version :=
		c.Param("namespace"), c.Param("name"), c.Param("provider"), c.Param("version")

	var m Module
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, namespace, name, provider, version, readme, yanked, published_at
		FROM registry_modules
		WHERE org_id=$1 AND namespace=$2 AND name=$3 AND provider=$4 AND version=$5
	`, orgID, ns, name, provider, version).Scan(
		&m.ID, &m.Namespace, &m.Name, &m.Provider, &m.Version,
		&m.Readme, &m.Yanked, &m.PublishedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "module version not found")
	}

	return c.JSON(http.StatusOK, map[string]any{
		"id":           fmt.Sprintf("%s/%s/%s/%s", ns, name, provider, version),
		"namespace":    m.Namespace,
		"name":         m.Name,
		"provider":     m.Provider,
		"version":      m.Version,
		"published_at": m.PublishedAt,
		"downloads":    0,
		"verified":     false,
	})
}

// Download implements GET /registry/v1/modules/:namespace/:name/:provider/:version/download
// Returns 204 with X-Terraform-Get pointing to the archive endpoint.
func (h *Handler) Download(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns, name, provider, version :=
		c.Param("namespace"), c.Param("name"), c.Param("provider"), c.Param("version")

	var exists bool
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT TRUE FROM registry_modules
		WHERE org_id=$1 AND namespace=$2 AND name=$3 AND provider=$4 AND version=$5 AND NOT yanked
	`, orgID, ns, name, provider, version).Scan(&exists)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "module version not found")
	}

	archiveURL := fmt.Sprintf("%s/registry/v1/modules/%s/%s/%s/%s/archive",
		h.cfg.BaseURL, ns, name, provider, version)
	c.Response().Header().Set("X-Terraform-Get", archiveURL)
	return c.NoContent(http.StatusNoContent)
}

// Archive implements GET /registry/v1/modules/:namespace/:name/:provider/:version/archive
// Streams the module tar.gz directly from MinIO.
func (h *Handler) Archive(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns, name, provider, version :=
		c.Param("namespace"), c.Param("name"), c.Param("provider"), c.Param("version")

	var storageKey string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT storage_key FROM registry_modules
		WHERE org_id=$1 AND namespace=$2 AND name=$3 AND provider=$4 AND version=$5 AND NOT yanked
	`, orgID, ns, name, provider, version).Scan(&storageKey)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "module version not found")
	}

	_, _ = h.pool.Exec(c.Request().Context(), `
		UPDATE registry_modules SET download_count = download_count + 1
		WHERE org_id=$1 AND namespace=$2 AND name=$3 AND provider=$4 AND version=$5
	`, orgID, ns, name, provider, version)

	obj, err := h.storage.GetModule(c.Request().Context(), storageKey)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch module archive")
	}
	defer obj.Close()

	filename := fmt.Sprintf("%s-%s-%s-%s.tar.gz", ns, name, provider, version)
	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.Stream(http.StatusOK, "application/gzip", obj)
}

// Search implements GET /registry/v1/modules/search?q=...
func (h *Handler) Search(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	q := c.QueryParam("q")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT namespace, name, provider, MAX(version) AS latest_version,
		       MAX(published_at) AS published_at
		FROM registry_modules
		WHERE org_id=$1 AND NOT yanked
		  AND ($2 = '' OR name ILIKE '%'||$2||'%' OR namespace ILIKE '%'||$2||'%')
		GROUP BY namespace, name, provider
		ORDER BY namespace, name, provider
		LIMIT 50
	`, orgID, q)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "search failed")
	}
	defer rows.Close()

	type result struct {
		ID          string    `json:"id"`
		Namespace   string    `json:"namespace"`
		Name        string    `json:"name"`
		Provider    string    `json:"provider"`
		Version     string    `json:"version"`
		PublishedAt time.Time `json:"published_at"`
	}
	results := []result{}
	for rows.Next() {
		var r result
		if err := rows.Scan(&r.Namespace, &r.Name, &r.Provider, &r.Version, &r.PublishedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		r.ID = fmt.Sprintf("%s/%s/%s", r.Namespace, r.Name, r.Provider)
		results = append(results, r)
	}

	return c.JSON(http.StatusOK, map[string]any{
		"meta":    map[string]any{"limit": 50, "current_offset": 0},
		"modules": results,
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func validateModuleFields(namespace, name, provider, version string) error {
	switch {
	case namespace == "":
		return fmt.Errorf("namespace is required")
	case !reName.MatchString(namespace):
		return fmt.Errorf("namespace must be lowercase alphanumeric with dashes")
	case name == "":
		return fmt.Errorf("name is required")
	case !reName.MatchString(name):
		return fmt.Errorf("name must be lowercase alphanumeric with dashes")
	case provider == "":
		return fmt.Errorf("provider is required")
	case !reName.MatchString(provider):
		return fmt.Errorf("provider must be lowercase alphanumeric with dashes")
	case !reVersion.MatchString(version):
		return fmt.Errorf("version must be semver (e.g. 1.2.3)")
	}
	return nil
}

// extractReadme scans a tar.gz archive for README.md and returns its text.
func extractReadme(data []byte) string {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		if strings.EqualFold(path.Base(hdr.Name), "readme.md") {
			b, _ := io.ReadAll(io.LimitReader(tr, 512<<10))
			return string(b)
		}
	}
	return ""
}
