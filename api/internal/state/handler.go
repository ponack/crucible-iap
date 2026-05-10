// SPDX-License-Identifier: AGPL-3.0-or-later
// Implements the Terraform HTTP backend protocol.
// Spec: https://developer.hashicorp.com/terraform/language/backend/http
package state

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/minio/minio-go/v7"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/statebackend"
	"github.com/ponack/crucible-iap/internal/storage"
	"github.com/ponack/crucible-iap/internal/vault"
)

type Handler struct {
	pool    *pgxpool.Pool
	storage *storage.Client
	vault   *vault.Vault
}

type LockInfo struct {
	ID        string    `json:"ID"`
	Operation string    `json:"Operation"`
	Info      string    `json:"Info"`
	Who       string    `json:"Who"`
	Version   string    `json:"Version"`
	Created   time.Time `json:"Created"`
	Path      string    `json:"Path"`
}

func NewHandler(pool *pgxpool.Pool, s *storage.Client, v *vault.Vault) *Handler {
	return &Handler{pool: pool, storage: s, vault: v}
}

// resolveBackend returns the external backend for a stack, or nil when none is
// configured (caller uses MinIO).
func (h *Handler) resolveBackend(ctx context.Context, stackID string) (statebackend.Backend, error) {
	b, err := statebackend.Resolve(ctx, h.pool, h.vault, stackID)
	if statebackend.IsNoOverride(err) {
		return nil, nil
	}
	return b, err
}

// GET /api/v1/state/:stackID — fetch current state
func (h *Handler) Get(c echo.Context) error {
	stackID := c.Param("stackID")
	ctx := c.Request().Context()

	if b, err := h.resolveBackend(ctx, stackID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	} else if b != nil {
		rc, err := b.GetState(ctx, stackID)
		if statebackend.IsNotFound(err) {
			return c.NoContent(http.StatusNoContent)
		}
		if err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		defer rc.Close()
		c.Response().Header().Set(echo.HeaderContentType, "application/json")
		_, err = io.Copy(c.Response(), rc)
		return err
	}

	// MinIO fallback.
	obj, err := h.storage.GetState(ctx, stackID)
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return c.NoContent(http.StatusNoContent)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer obj.Close()

	// MinIO GetObject is lazy — the actual HTTP request happens on the first
	// Read, not on GetObject. Buffer the content so we can detect "NoSuchKey"
	// (empty state on first run) before committing the response status code.
	// State files are small (KB–MB), so in-memory buffering is fine.
	data, err := io.ReadAll(obj)
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return c.NoContent(http.StatusNoContent)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.Blob(http.StatusOK, "application/json", data)
}

// StateResource is a single resource entry extracted from Terraform state.
type StateResource struct {
	Address       string `json:"address"`
	Type          string `json:"type"`
	Name          string `json:"name"`
	Module        string `json:"module,omitempty"`
	Mode          string `json:"mode"`
	InstanceCount int    `json:"instance_count"`
}

// ListResources parses the current state and returns the resource list.
// GET /api/v1/stacks/:id/state/resources — JWT auth, any org member.
func (h *Handler) ListResources(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var exists bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)
	`, stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	data, err := h.readState(ctx, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if data == nil {
		return c.JSON(http.StatusOK, []StateResource{})
	}

	var raw struct {
		Resources []struct {
			Module    string `json:"module"`
			Mode      string `json:"mode"`
			Type      string `json:"type"`
			Name      string `json:"name"`
			Instances []any  `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to parse state")
	}

	out := make([]StateResource, 0, len(raw.Resources))
	for _, r := range raw.Resources {
		addr := r.Type + "." + r.Name
		if r.Module != "" {
			addr = r.Module + "." + addr
		}
		out = append(out, StateResource{
			Address:       addr,
			Type:          r.Type,
			Name:          r.Name,
			Module:        r.Module,
			Mode:          r.Mode,
			InstanceCount: len(r.Instances),
		})
	}
	return c.JSON(http.StatusOK, out)
}

// readState loads raw state bytes from MinIO or the external backend.
// Returns nil, nil when no state has been written yet.
func (h *Handler) readState(ctx context.Context, stackID string) ([]byte, error) {
	if b, err := h.resolveBackend(ctx, stackID); err != nil {
		return nil, err
	} else if b != nil {
		rc, err := b.GetState(ctx, stackID)
		if statebackend.IsNotFound(err) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}

	obj, err := h.storage.GetState(ctx, stackID)
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil, nil
		}
		return nil, err
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return nil, nil
		}
		return nil, err
	}
	return data, nil
}

// POST /api/v1/state/:stackID — update state (caller must hold the lock)
func (h *Handler) Update(c echo.Context) error {
	stackID := c.Param("stackID")
	lockID := c.QueryParam("ID")
	ctx := c.Request().Context()

	if err := h.assertLockHolder(ctx, stackID, lockID); err != nil {
		return echo.NewHTTPError(http.StatusConflict, "lock ID mismatch")
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	if b, err := h.resolveBackend(ctx, stackID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	} else if b != nil {
		if err := b.PutState(ctx, stackID, body); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		go h.snapshotVersion(context.Background(), stackID, body)
		return c.NoContent(http.StatusOK)
	}

	if err := h.storage.PutState(ctx, stackID, bytes.NewReader(body), int64(len(body))); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	go h.snapshotVersion(context.Background(), stackID, body)
	return c.NoContent(http.StatusOK)
}

// snapshotVersion writes a versioned copy of the state to MinIO and records
// it in state_versions. Called asynchronously so it never delays the HTTP response.
func (h *Handler) snapshotVersion(ctx context.Context, stackID string, body []byte) {
	var raw struct {
		Serial    int64 `json:"serial"`
		Resources []struct {
			Mode string `json:"mode"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		slog.Warn("state snapshot: failed to parse state JSON", "stack_id", stackID, "err", err)
		return
	}
	managed := 0
	for _, r := range raw.Resources {
		if r.Mode == "managed" {
			managed++
		}
	}

	versionID := uuid.New().String()
	if err := h.storage.PutStateVersion(ctx, stackID, versionID, body); err != nil {
		slog.Warn("state snapshot: failed to write to storage", "stack_id", stackID, "err", err)
		return
	}

	var runID *string
	var rid string
	if err := h.pool.QueryRow(ctx, `
		SELECT id FROM runs
		WHERE stack_id = $1 AND status = 'applying'
		ORDER BY queued_at DESC LIMIT 1
	`, stackID).Scan(&rid); err == nil {
		runID = &rid
	}

	storageKey := stackID + "/versions/" + versionID + ".json"
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO state_versions (id, stack_id, run_id, serial, storage_key, resource_count)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (stack_id, serial) DO NOTHING
	`, versionID, stackID, runID, raw.Serial, storageKey, managed); err != nil {
		slog.Warn("state snapshot: failed to insert record", "stack_id", stackID, "err", err)
	}
}

// DELETE /api/v1/state/:stackID — purge state
func (h *Handler) Delete(c echo.Context) error {
	stackID := c.Param("stackID")
	ctx := c.Request().Context()

	if b, err := h.resolveBackend(ctx, stackID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	} else if b != nil {
		if err := b.DeleteState(ctx, stackID); err != nil {
			return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
		}
		return c.NoContent(http.StatusOK)
	}

	if err := h.storage.DeleteState(ctx, stackID); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusOK)
}

// Lock — LOCK /api/v1/state/:stackID — acquire state lock
func (h *Handler) Lock(c echo.Context) error {
	stackID := c.Param("stackID")

	var info LockInfo
	if err := json.NewDecoder(c.Request().Body).Decode(&info); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	info.Created = time.Now()

	infoJSON, _ := json.Marshal(info)
	_, err := h.pool.Exec(c.Request().Context(), `
		INSERT INTO state_locks (stack_id, lock_id, operation, holder_info)
		VALUES ($1, $2, $3, $4)
	`, stackID, info.ID, info.Operation, infoJSON)
	if err != nil {
		// Unique violation — already locked; return existing lock info with 423
		var existing LockInfo
		_ = h.pool.QueryRow(c.Request().Context(), `
			SELECT holder_info FROM state_locks WHERE stack_id = $1
		`, stackID).Scan(&existing)
		return c.JSON(http.StatusLocked, existing)
	}
	return c.JSON(http.StatusOK, info)
}

// Unlock — UNLOCK /api/v1/state/:stackID — release state lock
func (h *Handler) Unlock(c echo.Context) error {
	stackID := c.Param("stackID")

	var info LockInfo
	if err := json.NewDecoder(c.Request().Body).Decode(&info); err != nil || info.ID == "" {
		// OpenTofu always sends a valid lock ID in the UNLOCK body. An empty or
		// unparseable body indicates a malformed request; reject it so callers
		// cannot clear an arbitrary stack lock without knowing the lock ID.
		return echo.NewHTTPError(http.StatusBadRequest, "lock ID is required")
	}

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM state_locks WHERE stack_id = $1 AND lock_id = $2
	`, stackID, info.ID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	if tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "lock not found or ID mismatch")
	}
	return c.NoContent(http.StatusOK)
}

// ForceUnlock clears a stuck state lock for a stack.
// For emergency use when a runner container exited without releasing the lock
// (e.g. container killed mid-apply, network interruption during UNLOCK).
// DELETE /api/v1/stacks/:id/lock — admin role required.
func (h *Handler) ForceUnlock(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	userID, _ := c.Get("userID").(string)

	// Verify the stack belongs to this org.
	var exists bool
	if err := h.pool.QueryRow(c.Request().Context(), `
		SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)
	`, stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	var lockID string
	err := h.pool.QueryRow(c.Request().Context(), `
		DELETE FROM state_locks WHERE stack_id = $1 RETURNING lock_id
	`, stackID).Scan(&lockID)
	if err == pgx.ErrNoRows {
		return echo.NewHTTPError(http.StatusNotFound, "no lock held on this stack")
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	audit.Record(c.Request().Context(), h.pool, audit.Event{
		ActorID:      userID,
		Action:       "stack.lock.force-unlocked",
		ResourceID:   stackID,
		ResourceType: "stack",
	})
	return c.JSON(http.StatusOK, map[string]string{"cleared_lock_id": lockID})
}

// StateVersion is one recorded state snapshot for a stack.
type StateVersion struct {
	ID            string     `json:"id"`
	RunID         *string    `json:"run_id,omitempty"`
	Serial        int64      `json:"serial"`
	ResourceCount int        `json:"resource_count"`
	CreatedAt     time.Time  `json:"created_at"`
}

// StateDiff is the structured diff between two consecutive state versions.
type StateDiff struct {
	FromVersionID *string          `json:"from_version_id"`
	ToVersionID   string           `json:"to_version_id"`
	Added         []DiffEntry      `json:"added"`
	Removed       []DiffEntry      `json:"removed"`
	Changed       []ChangedEntry   `json:"changed"`
}

// DiffEntry represents a resource that was purely added or removed.
type DiffEntry struct {
	Address       string `json:"address"`
	Type          string `json:"type"`
	InstanceCount int    `json:"instance_count"`
}

// ChangedEntry represents a resource present in both versions with differing attributes.
type ChangedEntry struct {
	Address string         `json:"address"`
	Type    string         `json:"type"`
	Before  map[string]any `json:"before"`
	After   map[string]any `json:"after"`
}

// ListVersions returns state version history for a stack, newest first.
// GET /api/v1/stacks/:id/state/versions
func (h *Handler) ListVersions(c echo.Context) error {
	stackID := c.Param("id")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var exists bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)
	`, stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	rows, err := h.pool.Query(ctx, `
		SELECT id, run_id, serial, resource_count, created_at
		FROM state_versions
		WHERE stack_id = $1
		ORDER BY created_at DESC
		LIMIT 50
	`, stackID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer rows.Close()

	versions := []StateVersion{}
	for rows.Next() {
		var v StateVersion
		if err := rows.Scan(&v.ID, &v.RunID, &v.Serial, &v.ResourceCount, &v.CreatedAt); err != nil {
			continue
		}
		versions = append(versions, v)
	}
	return c.JSON(http.StatusOK, versions)
}

// GetVersionDiff computes the diff between a state version and its predecessor.
// GET /api/v1/stacks/:id/state/versions/:versionID/diff
func (h *Handler) GetVersionDiff(c echo.Context) error {
	stackID := c.Param("id")
	versionID := c.Param("versionID")
	orgID := c.Get("orgID").(string)
	ctx := c.Request().Context()

	var exists bool
	if err := h.pool.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM stacks WHERE id = $1 AND org_id = $2)
	`, stackID, orgID).Scan(&exists); err != nil || !exists {
		return echo.NewHTTPError(http.StatusNotFound, "stack not found")
	}

	// Load the target version.
	var targetSerial int64
	var targetKey string
	if err := h.pool.QueryRow(ctx, `
		SELECT serial, storage_key FROM state_versions WHERE id = $1 AND stack_id = $2
	`, versionID, stackID).Scan(&targetSerial, &targetKey); err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "state version not found")
	}

	// Load the predecessor (next-lower serial for this stack).
	var prevID *string
	var prevKey *string
	var pid, pkey string
	if err := h.pool.QueryRow(ctx, `
		SELECT id, storage_key FROM state_versions
		WHERE stack_id = $1 AND serial < $2
		ORDER BY serial DESC LIMIT 1
	`, stackID, targetSerial).Scan(&pid, &pkey); err == nil {
		prevID = &pid
		prevKey = &pkey
	}

	toData, err := h.storage.GetStateVersion(ctx, stackID, versionID)
	if err != nil || toData == nil {
		return echo.NewHTTPError(http.StatusNotFound, "state snapshot not available")
	}

	diff := StateDiff{
		FromVersionID: prevID,
		ToVersionID:   versionID,
		Added:         []DiffEntry{},
		Removed:       []DiffEntry{},
		Changed:       []ChangedEntry{},
	}

	if prevKey == nil {
		// First version — everything is "added".
		newRes := parseStateResources(toData)
		for addr, r := range newRes {
			diff.Added = append(diff.Added, DiffEntry{Address: addr, Type: r.rtype, InstanceCount: r.count})
		}
		return c.JSON(http.StatusOK, diff)
	}

	// Extract versionID from storage key: "{stackID}/versions/{versionID}.json"
	prevVersionID := extractVersionID(*prevKey)
	fromData, err := h.storage.GetStateVersion(ctx, stackID, prevVersionID)
	if err != nil || fromData == nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "previous state snapshot not available")
	}

	oldRes := parseStateResources(fromData)
	newRes := parseStateResources(toData)

	// Added.
	for addr, r := range newRes {
		if _, ok := oldRes[addr]; !ok {
			diff.Added = append(diff.Added, DiffEntry{Address: addr, Type: r.rtype, InstanceCount: r.count})
		}
	}
	// Removed.
	for addr, r := range oldRes {
		if _, ok := newRes[addr]; !ok {
			diff.Removed = append(diff.Removed, DiffEntry{Address: addr, Type: r.rtype, InstanceCount: r.count})
		}
	}
	// Changed.
	for addr, nr := range newRes {
		or, ok := oldRes[addr]
		if !ok {
			continue
		}
		before, after, changed := diffAttrs(or.attrs, nr.attrs)
		if changed {
			diff.Changed = append(diff.Changed, ChangedEntry{
				Address: addr, Type: nr.rtype, Before: before, After: after,
			})
		}
	}

	return c.JSON(http.StatusOK, diff)
}

// ── State diff helpers ────────────────────────────────────────────────────────

type parsedResource struct {
	rtype string
	count int
	attrs map[string]any // merged attributes of all instances
}

func parseStateResources(data []byte) map[string]parsedResource {
	var raw struct {
		Resources []struct {
			Module    string `json:"module"`
			Mode      string `json:"mode"`
			Type      string `json:"type"`
			Name      string `json:"name"`
			Instances []struct {
				Attributes map[string]any `json:"attributes"`
			} `json:"instances"`
		} `json:"resources"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	out := make(map[string]parsedResource, len(raw.Resources))
	for _, r := range raw.Resources {
		if r.Mode != "managed" {
			continue
		}
		addr := r.Type + "." + r.Name
		if r.Module != "" {
			addr = r.Module + "." + addr
		}
		merged := map[string]any{}
		for _, inst := range r.Instances {
			for k, v := range inst.Attributes {
				merged[k] = v
			}
		}
		out[addr] = parsedResource{rtype: r.Type, count: len(r.Instances), attrs: merged}
	}
	return out
}

// diffAttrs returns only the attribute keys that changed between old and new,
// capping string values at 512 chars to keep diffs readable.
func diffAttrs(old, new map[string]any) (before, after map[string]any, changed bool) {
	before, after = map[string]any{}, map[string]any{}
	allKeys := map[string]struct{}{}
	for k := range old {
		allKeys[k] = struct{}{}
	}
	for k := range new {
		allKeys[k] = struct{}{}
	}
	for k := range allKeys {
		ov, inOld := old[k]
		nv, inNew := new[k]
		ov = truncate(ov)
		nv = truncate(nv)
		equal := fmt.Sprintf("%v", ov) == fmt.Sprintf("%v", nv)
		if equal && inOld == inNew {
			continue
		}
		changed = true
		if inOld {
			before[k] = ov
		}
		if inNew {
			after[k] = nv
		}
	}
	return before, after, changed
}

func truncate(v any) any {
	s, ok := v.(string)
	if ok && len(s) > 512 {
		return s[:512] + "…"
	}
	return v
}

func extractVersionID(storageKey string) string {
	// key format: "{stackID}/versions/{versionID}.json"
	parts := storageKey
	if idx := len(parts) - len(".json"); idx > 0 && parts[idx:] == ".json" {
		parts = parts[:idx]
	}
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == '/' {
			return parts[i+1:]
		}
	}
	return parts
}

func (h *Handler) assertLockHolder(ctx context.Context, stackID, lockID string) error {
	var storedLockID string
	err := h.pool.QueryRow(ctx, `SELECT lock_id FROM state_locks WHERE stack_id = $1`, stackID).Scan(&storedLockID)
	if err == pgx.ErrNoRows {
		return nil // not locked, allow
	}
	if err != nil {
		return err
	}
	if lockID != "" && storedLockID != lockID {
		return echo.NewHTTPError(http.StatusConflict, "lock ID mismatch")
	}
	return nil
}
