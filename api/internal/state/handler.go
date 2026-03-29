// SPDX-License-Identifier: AGPL-3.0-or-later
// Implements the Terraform HTTP backend protocol.
// Spec: https://developer.hashicorp.com/terraform/language/backend/http
package state

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/minio/minio-go/v7"
	"github.com/ponack/crucible/internal/config"
)

type Handler struct {
	pool   *pgxpool.Pool
	minio  *minio.Client
	bucket string
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

func NewHandler(pool *pgxpool.Pool, cfg *config.Config) *Handler {
	// TODO: initialise MinIO client from cfg
	return &Handler{pool: pool, bucket: cfg.MinioBucketState}
}

// GET /api/v1/state/:stackID — fetch current state
func (h *Handler) Get(c echo.Context) error {
	stackID := c.Param("stackID")
	obj, err := h.minio.GetObject(c.Request().Context(), h.bucket, stateKey(stackID), minio.GetObjectOptions{})
	if err != nil {
		resp := minio.ToErrorResponse(err)
		if resp.Code == "NoSuchKey" {
			return c.NoContent(http.StatusNoContent)
		}
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	defer obj.Close()

	c.Response().Header().Set(echo.HeaderContentType, "application/json")
	_, err = io.Copy(c.Response(), obj)
	return err
}

// POST /api/v1/state/:stackID — update state (must hold lock)
func (h *Handler) Update(c echo.Context) error {
	stackID := c.Param("stackID")
	lockID := c.QueryParam("ID")

	// Verify caller holds the lock
	if err := h.assertLockHolder(c.Request().Context(), stackID, lockID); err != nil {
		return echo.NewHTTPError(http.StatusConflict, "lock ID mismatch")
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	_, err = h.minio.PutObject(c.Request().Context(), h.bucket, stateKey(stackID),
		io.NopCloser(io.Reader(bytesReader(body))),
		int64(len(body)), minio.PutObjectOptions{ContentType: "application/json"})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusOK)
}

// DELETE /api/v1/state/:stackID — purge state
func (h *Handler) Delete(c echo.Context) error {
	stackID := c.Param("stackID")
	err := h.minio.RemoveObject(c.Request().Context(), h.bucket, stateKey(stackID), minio.RemoveObjectOptions{})
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return c.NoContent(http.StatusOK)
}

// LOCK /api/v1/state/:stackID — acquire state lock
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
		// Unique violation — already locked; return existing lock info
		var existing LockInfo
		_ = h.pool.QueryRow(c.Request().Context(), `
			SELECT holder_info FROM state_locks WHERE stack_id = $1
		`, stackID).Scan(&existing)
		c.Response().WriteHeader(http.StatusLocked) // 423
		return c.JSON(http.StatusLocked, existing)
	}
	return c.JSON(http.StatusOK, info)
}

// UNLOCK /api/v1/state/:stackID — release state lock
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
	if storedLockID != lockID {
		return echo.NewHTTPError(http.StatusConflict, "lock ID mismatch")
	}
	return nil
}

func stateKey(stackID string) string { return stackID + "/terraform.tfstate" }

type bytesReaderWrapper struct{ *io.Reader }

func bytesReader(b []byte) io.Reader {
	r := io.NopCloser(nil)
	_ = r
	// Simple bytes reader
	buf := make([]byte, len(b))
	copy(buf, b)
	return &simpleReader{data: buf}
}

type simpleReader struct {
	data   []byte
	offset int
}

func (r *simpleReader) Read(p []byte) (n int, err error) {
	if r.offset >= len(r.data) {
		return 0, io.EOF
	}
	n = copy(p, r.data[r.offset:])
	r.offset += n
	return n, nil
}
