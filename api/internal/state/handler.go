// SPDX-License-Identifier: AGPL-3.0-or-later
// Implements the Terraform HTTP backend protocol.
// Spec: https://developer.hashicorp.com/terraform/language/backend/http
package state

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/minio/minio-go/v7"
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
		return c.NoContent(http.StatusOK)
	}

	if err := h.storage.PutState(ctx, stackID, bytes.NewReader(body), int64(len(body))); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusOK)
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
	if err := json.NewDecoder(c.Request().Body).Decode(&info); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	tag, err := h.pool.Exec(c.Request().Context(), `
		DELETE FROM state_locks WHERE stack_id = $1 AND lock_id = $2
	`, stackID, info.ID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.NewHTTPError(http.StatusConflict, "lock not found or ID mismatch")
	}
	return c.NoContent(http.StatusOK)
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
