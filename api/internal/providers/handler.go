// SPDX-License-Identifier: AGPL-3.0-or-later
// Package providers implements the Terraform Provider Registry Protocol v1 and
// the management API for publishing and yanking private provider binaries.
package providers

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/storage"
)

var (
	reName    = regexp.MustCompile(`^[a-z0-9][a-z0-9\-]{0,63}$`)
	reVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+`)
	reOSArch  = regexp.MustCompile(`^[a-z0-9_]+$`)
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

type Provider struct {
	ID            string    `json:"id"`
	Namespace     string    `json:"namespace"`
	Type          string    `json:"type"`
	Version       string    `json:"version"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	Filename      string    `json:"filename"`
	Shasum        string    `json:"shasum"`
	Protocols     []string  `json:"protocols"`
	Readme        string    `json:"readme,omitempty"`
	Yanked        bool      `json:"yanked"`
	PublishedBy   string    `json:"published_by,omitempty"`
	PublishedAt   time.Time `json:"published_at"`
	DownloadCount int64     `json:"download_count"`
}

type GPGKey struct {
	ID         string    `json:"id"`
	Namespace  string    `json:"namespace"`
	KeyID      string    `json:"key_id"`
	AsciiArmor string    `json:"ascii_armor"`
	CreatedBy  string    `json:"created_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// ── Management API ────────────────────────────────────────────────────────────

func (h *Handler) List(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	q := c.QueryParam("q")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT p.id, p.namespace, p.type, p.version, p.os, p.arch,
		       p.filename, p.shasum, p.protocols, p.readme, p.yanked,
		       COALESCE(u.email,'') AS published_by, p.published_at, p.download_count
		FROM registry_providers p
		LEFT JOIN users u ON u.id = p.published_by
		WHERE p.org_id = $1
		  AND ($2 = '' OR p.type ILIKE '%'||$2||'%' OR p.namespace ILIKE '%'||$2||'%')
		ORDER BY p.namespace, p.type, p.version DESC, p.os, p.arch
	`, orgID, q)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list providers")
	}
	defer rows.Close()

	result := []Provider{}
	for rows.Next() {
		var p Provider
		if err := rows.Scan(&p.ID, &p.Namespace, &p.Type, &p.Version, &p.OS, &p.Arch,
			&p.Filename, &p.Shasum, &p.Protocols, &p.Readme, &p.Yanked,
			&p.PublishedBy, &p.PublishedAt, &p.DownloadCount); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		result = append(result, p)
	}
	return c.JSON(http.StatusOK, result)
}

func (h *Handler) Get(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	var p Provider
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT p.id, p.namespace, p.type, p.version, p.os, p.arch,
		       p.filename, p.shasum, p.protocols, p.readme, p.yanked,
		       COALESCE(u.email,'') AS published_by, p.published_at, p.download_count
		FROM registry_providers p
		LEFT JOIN users u ON u.id = p.published_by
		WHERE p.id = $1 AND p.org_id = $2
	`, id, orgID).Scan(&p.ID, &p.Namespace, &p.Type, &p.Version, &p.OS, &p.Arch,
		&p.Filename, &p.Shasum, &p.Protocols, &p.Readme, &p.Yanked,
		&p.PublishedBy, &p.PublishedAt, &p.DownloadCount)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "provider not found")
	}
	return c.JSON(http.StatusOK, p)
}

func (h *Handler) Publish(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	ns := c.FormValue("namespace")
	ptype := c.FormValue("type")
	version := c.FormValue("version")
	osName := c.FormValue("os")
	arch := c.FormValue("arch")
	readme := c.FormValue("readme")

	if err := validateFields(ns, ptype, version, osName, arch); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	file, _, err := c.Request().FormFile("provider")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "provider file is required")
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, 512<<20))
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "failed to read provider file")
	}

	sum := sha256.Sum256(data)
	shasum := hex.EncodeToString(sum[:])
	filename := fmt.Sprintf("terraform-provider-%s_%s_%s_%s.zip", ptype, version, osName, arch)
	key := storage.ProviderKey(ns, ptype, version, osName, arch)

	if err := h.storage.PutProvider(c.Request().Context(), key, bytes.NewReader(data), int64(len(data))); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to store provider")
	}

	p, err := upsertProvider(c, h.pool, orgID, userID, ns, ptype, version, osName, arch, filename, key, shasum, readme)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to record provider")
	}
	return c.JSON(http.StatusCreated, p)
}

func upsertProvider(c echo.Context, pool *pgxpool.Pool, orgID, userID, ns, ptype, version, osName, arch, filename, key, shasum, readme string) (Provider, error) {
	var p Provider
	err := pool.QueryRow(c.Request().Context(), `
		INSERT INTO registry_providers
		  (org_id, namespace, type, version, os, arch, filename, storage_key, shasum, readme, published_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		ON CONFLICT (org_id, namespace, type, version, os, arch)
		DO UPDATE SET storage_key=$8, shasum=$9, readme=$10, published_by=$11, published_at=NOW(), yanked=FALSE
		RETURNING id, namespace, type, version, os, arch, filename, shasum, protocols,
		          readme, yanked, published_at, download_count
	`, orgID, ns, ptype, version, osName, arch, filename, key, shasum, readme, userID).Scan(
		&p.ID, &p.Namespace, &p.Type, &p.Version, &p.OS, &p.Arch,
		&p.Filename, &p.Shasum, &p.Protocols, &p.Readme, &p.Yanked,
		&p.PublishedAt, &p.DownloadCount)
	if err != nil {
		return p, err
	}
	p.PublishedBy = userID
	return p, nil
}

func (h *Handler) Yank(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE registry_providers SET yanked=TRUE WHERE id=$1 AND org_id=$2`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "provider not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// ── GPG key management ────────────────────────────────────────────────────────

func (h *Handler) ListGPGKeys(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT k.id, k.namespace, k.key_id, k.ascii_armor,
		       COALESCE(u.email,'') AS created_by, k.created_at
		FROM registry_provider_gpg_keys k
		LEFT JOIN users u ON u.id = k.created_by
		WHERE k.org_id = $1
		ORDER BY k.namespace, k.created_at DESC
	`, orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to list GPG keys")
	}
	defer rows.Close()

	keys := []GPGKey{}
	for rows.Next() {
		var k GPGKey
		if err := rows.Scan(&k.ID, &k.Namespace, &k.KeyID, &k.AsciiArmor, &k.CreatedBy, &k.CreatedAt); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		keys = append(keys, k)
	}
	return c.JSON(http.StatusOK, keys)
}

func (h *Handler) AddGPGKey(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	var body struct {
		Namespace  string `json:"namespace"`
		KeyID      string `json:"key_id"`
		AsciiArmor string `json:"ascii_armor"`
	}
	if err := c.Bind(&body); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}
	if body.Namespace == "" || body.KeyID == "" || body.AsciiArmor == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "namespace, key_id, and ascii_armor are required")
	}
	if !reName.MatchString(body.Namespace) {
		return echo.NewHTTPError(http.StatusBadRequest, "namespace must be lowercase alphanumeric with dashes")
	}

	var k GPGKey
	err := h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO registry_provider_gpg_keys (org_id, namespace, key_id, ascii_armor, created_by)
		VALUES ($1,$2,$3,$4,$5)
		ON CONFLICT (org_id, namespace, key_id) DO UPDATE SET ascii_armor=$4, created_by=$5, created_at=NOW()
		RETURNING id, namespace, key_id, ascii_armor, created_at
	`, orgID, body.Namespace, body.KeyID, body.AsciiArmor, userID).Scan(
		&k.ID, &k.Namespace, &k.KeyID, &k.AsciiArmor, &k.CreatedAt)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to save GPG key")
	}
	k.CreatedBy = userID
	return c.JSON(http.StatusCreated, k)
}

func (h *Handler) DeleteGPGKey(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	id := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM registry_provider_gpg_keys WHERE id=$1 AND org_id=$2`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "GPG key not found")
	}
	return c.NoContent(http.StatusNoContent)
}

// ── Terraform Provider Registry Protocol v1 ──────────────────────────────────

// Versions implements GET /registry/v1/providers/:namespace/:type/versions
func (h *Handler) Versions(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns, ptype := c.Param("namespace"), c.Param("type")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT version, os, arch, protocols
		FROM registry_providers
		WHERE org_id=$1 AND namespace=$2 AND type=$3 AND NOT yanked
		ORDER BY published_at DESC
	`, orgID, ns, ptype)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "query failed")
	}
	defer rows.Close()

	type platform struct {
		OS   string `json:"os"`
		Arch string `json:"arch"`
	}
	type versionEntry struct {
		Version   string     `json:"version"`
		Protocols []string   `json:"protocols"`
		Platforms []platform `json:"platforms"`
	}

	vmap := map[string]*versionEntry{}
	vorder := []string{}
	for rows.Next() {
		var ver, osName, arch string
		var protocols []string
		if err := rows.Scan(&ver, &osName, &arch, &protocols); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		if _, ok := vmap[ver]; !ok {
			vmap[ver] = &versionEntry{Version: ver, Protocols: protocols, Platforms: []platform{}}
			vorder = append(vorder, ver)
		}
		vmap[ver].Platforms = append(vmap[ver].Platforms, platform{OS: osName, Arch: arch})
	}

	if len(vorder) == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no versions found")
	}

	versions := make([]versionEntry, 0, len(vorder))
	for _, v := range vorder {
		versions = append(versions, *vmap[v])
	}
	return c.JSON(http.StatusOK, map[string]any{"versions": versions})
}

// DownloadInfo implements GET /registry/v1/providers/:namespace/:type/:version/download/:os/:arch
func (h *Handler) DownloadInfo(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns := c.Param("namespace")
	ptype := c.Param("type")
	version := c.Param("version")
	osName := c.Param("os")
	arch := c.Param("arch")

	var p Provider
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT filename, shasum, protocols
		FROM registry_providers
		WHERE org_id=$1 AND namespace=$2 AND type=$3 AND version=$4 AND os=$5 AND arch=$6 AND NOT yanked
	`, orgID, ns, ptype, version, osName, arch).Scan(&p.Filename, &p.Shasum, &p.Protocols)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "provider platform not found")
	}

	gpgKeys := h.loadGPGKeys(c, orgID, ns)

	base := h.cfg.BaseURL
	archiveURL := fmt.Sprintf("%s/registry/v1/providers/%s/%s/%s/archive/%s/%s", base, ns, ptype, version, osName, arch)
	shasumsURL := fmt.Sprintf("%s/registry/v1/providers/%s/%s/%s/shasums", base, ns, ptype, version)
	shaSigURL := fmt.Sprintf("%s/registry/v1/providers/%s/%s/%s/shasums.sig", base, ns, ptype, version)

	return c.JSON(http.StatusOK, map[string]any{
		"protocols":              p.Protocols,
		"os":                     osName,
		"arch":                   arch,
		"filename":               p.Filename,
		"download_url":           archiveURL,
		"shasums_url":            shasumsURL,
		"shasums_signature_url":  shaSigURL,
		"shasum":                 p.Shasum,
		"signing_keys":           map[string]any{"gpg_public_keys": gpgKeys},
	})
}

func (h *Handler) loadGPGKeys(c echo.Context, orgID, namespace string) []map[string]any {
	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT key_id, ascii_armor FROM registry_provider_gpg_keys
		WHERE org_id=$1 AND namespace=$2
	`, orgID, namespace)
	if err != nil {
		return []map[string]any{}
	}
	defer rows.Close()

	keys := []map[string]any{}
	for rows.Next() {
		var keyID, asciiArmor string
		if rows.Scan(&keyID, &asciiArmor) != nil {
			continue
		}
		keys = append(keys, map[string]any{
			"key_id":      keyID,
			"ascii_armor": asciiArmor,
		})
	}
	return keys
}

// Archive implements GET /registry/v1/providers/:namespace/:type/:version/archive/:os/:arch
func (h *Handler) Archive(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns := c.Param("namespace")
	ptype := c.Param("type")
	version := c.Param("version")
	osName := c.Param("os")
	arch := c.Param("arch")

	var storageKey, filename string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT storage_key, filename FROM registry_providers
		WHERE org_id=$1 AND namespace=$2 AND type=$3 AND version=$4 AND os=$5 AND arch=$6 AND NOT yanked
	`, orgID, ns, ptype, version, osName, arch).Scan(&storageKey, &filename)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "provider platform not found")
	}

	_, _ = h.pool.Exec(c.Request().Context(), `
		UPDATE registry_providers SET download_count = download_count + 1
		WHERE org_id=$1 AND namespace=$2 AND type=$3 AND version=$4 AND os=$5 AND arch=$6
	`, orgID, ns, ptype, version, osName, arch)

	obj, err := h.storage.GetProvider(c.Request().Context(), storageKey)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to fetch provider archive")
	}
	defer obj.Close()

	c.Response().Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	return c.Stream(http.StatusOK, "application/zip", obj)
}

// Shasums implements GET /registry/v1/providers/:namespace/:type/:version/shasums
// Returns a dynamically generated SHA256SUMS file for all platforms of a version.
func (h *Handler) Shasums(c echo.Context) error {
	orgID, _ := c.Get("orgID").(string)
	ns, ptype, version := c.Param("namespace"), c.Param("type"), c.Param("version")

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT filename, shasum FROM registry_providers
		WHERE org_id=$1 AND namespace=$2 AND type=$3 AND version=$4 AND NOT yanked
		ORDER BY os, arch
	`, orgID, ns, ptype, version)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "query failed")
	}
	defer rows.Close()

	var sb strings.Builder
	for rows.Next() {
		var filename, shasum string
		if err := rows.Scan(&filename, &shasum); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, "scan failed")
		}
		fmt.Fprintf(&sb, "%s  %s\n", shasum, filename)
	}
	if sb.Len() == 0 {
		return echo.NewHTTPError(http.StatusNotFound, "no versions found")
	}
	return c.Blob(http.StatusOK, "text/plain; charset=utf-8", []byte(sb.String()))
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func validateFields(namespace, ptype, version, osName, arch string) error {
	switch {
	case namespace == "":
		return fmt.Errorf("namespace is required")
	case !reName.MatchString(namespace):
		return fmt.Errorf("namespace must be lowercase alphanumeric with dashes")
	case ptype == "":
		return fmt.Errorf("type is required")
	case !reName.MatchString(ptype):
		return fmt.Errorf("type must be lowercase alphanumeric with dashes")
	case !reVersion.MatchString(version):
		return fmt.Errorf("version must be semver (e.g. 1.2.3)")
	case osName == "" || !reOSArch.MatchString(osName):
		return fmt.Errorf("os must be lowercase alphanumeric (e.g. linux, darwin, windows)")
	case arch == "" || !reOSArch.MatchString(arch):
		return fmt.Errorf("arch must be lowercase alphanumeric (e.g. amd64, arm64)")
	}
	return nil
}
