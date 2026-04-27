// SPDX-License-Identifier: AGPL-3.0-or-later
// Package workerpools manages the external worker pool registry.
// Each pool is a named group of external agent processes that share a token
// and claim runs assigned to that pool via the /api/v1/agent endpoints.
package workerpools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/pagination"
	"golang.org/x/crypto/bcrypt"
)

type Handler struct {
	pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool}
}

type WorkerPool struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Capacity    int        `json:"capacity"`
	IsDisabled  bool       `json:"is_disabled"`
	LastSeenAt  *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (h *Handler) List(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	p := pagination.Parse(c)

	rows, err := h.pool.Query(c.Request().Context(), `
		SELECT id, name, description, capacity, is_disabled, last_seen_at, created_at,
		       COUNT(*) OVER() AS total
		FROM worker_pools WHERE org_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3
	`, orgID, p.Limit, p.Offset)
	if err != nil {
		return fmt.Errorf("list worker pools: %w", err)
	}
	defer rows.Close()

	var items []WorkerPool
	var total int
	for rows.Next() {
		var wp WorkerPool
		if err := rows.Scan(&wp.ID, &wp.Name, &wp.Description, &wp.Capacity,
			&wp.IsDisabled, &wp.LastSeenAt, &wp.CreatedAt, &total); err != nil {
			return fmt.Errorf("scan worker pool: %w", err)
		}
		items = append(items, wp)
	}
	return c.JSON(http.StatusOK, pagination.Wrap(items, p, total))
}

func (h *Handler) Create(c echo.Context) error {
	orgID := c.Get("orgID").(string)

	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Capacity    int    `json:"capacity"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if req.Name == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "name is required")
	}
	if req.Capacity <= 0 {
		req.Capacity = 3
	}

	token, hash, err := generateToken()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	var wp WorkerPool
	err = h.pool.QueryRow(c.Request().Context(), `
		INSERT INTO worker_pools (org_id, name, description, capacity, token_hash)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, name, description, capacity, is_disabled, last_seen_at, created_at
	`, orgID, req.Name, req.Description, req.Capacity, hash).Scan(
		&wp.ID, &wp.Name, &wp.Description, &wp.Capacity, &wp.IsDisabled, &wp.LastSeenAt, &wp.CreatedAt)
	if err != nil {
		return fmt.Errorf("create worker pool: %w", err)
	}

	return c.JSON(http.StatusCreated, map[string]any{
		"pool":  wp,
		"token": token, // returned once — store it securely
	})
}

func (h *Handler) Get(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")

	var wp WorkerPool
	if err := h.pool.QueryRow(c.Request().Context(), `
		SELECT id, name, description, capacity, is_disabled, last_seen_at, created_at
		FROM worker_pools WHERE id = $1 AND org_id = $2
	`, id, orgID).Scan(&wp.ID, &wp.Name, &wp.Description, &wp.Capacity,
		&wp.IsDisabled, &wp.LastSeenAt, &wp.CreatedAt); err != nil {
		return echo.ErrNotFound
	}
	return c.JSON(http.StatusOK, wp)
}

func (h *Handler) Update(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")

	var req struct {
		Description *string `json:"description"`
		Capacity    *int    `json:"capacity"`
		IsDisabled  *bool   `json:"is_disabled"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	sets := []string{}
	args := []any{}
	add := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if req.Description != nil {
		add("description", *req.Description)
	}
	if req.Capacity != nil {
		if *req.Capacity <= 0 {
			return echo.NewHTTPError(http.StatusBadRequest, "capacity must be > 0")
		}
		add("capacity", *req.Capacity)
	}
	if req.IsDisabled != nil {
		add("is_disabled", *req.IsDisabled)
	}
	if len(sets) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "no fields to update")
	}

	args = append(args, id, orgID)
	tag, err := h.pool.Exec(c.Request().Context(),
		fmt.Sprintf("UPDATE worker_pools SET %s WHERE id = $%d AND org_id = $%d",
			joinComma(sets), len(args)-1, len(args)),
		args...)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.ErrNotFound
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) Delete(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")

	tag, err := h.pool.Exec(c.Request().Context(),
		`DELETE FROM worker_pools WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.ErrNotFound
	}
	return c.NoContent(http.StatusNoContent)
}

func (h *Handler) RotateToken(c echo.Context) error {
	orgID := c.Get("orgID").(string)
	id := c.Param("id")

	token, hash, err := generateToken()
	if err != nil {
		return fmt.Errorf("generate token: %w", err)
	}

	tag, err := h.pool.Exec(c.Request().Context(),
		`UPDATE worker_pools SET token_hash = $1 WHERE id = $2 AND org_id = $3`, hash, id, orgID)
	if err != nil || tag.RowsAffected() == 0 {
		return echo.ErrNotFound
	}
	return c.JSON(http.StatusOK, map[string]string{"token": token})
}

// VerifyToken checks a plaintext bearer token against stored bcrypt hashes for
// pools in the given org. Returns the matching pool ID, or an error.
func VerifyToken(ctx context.Context, pool *pgxpool.Pool, orgID, bearer string) (string, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, token_hash FROM worker_pools WHERE org_id = $1 AND is_disabled = false`, orgID)
	if err != nil {
		return "", fmt.Errorf("query pools: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err != nil {
			continue
		}
		if bcrypt.CompareHashAndPassword([]byte(hash), []byte(bearer)) == nil {
			return id, nil
		}
	}
	return "", fmt.Errorf("invalid token")
}

func generateToken() (plaintext, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	plain := hex.EncodeToString(b)
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", "", err
	}
	return plain, string(h), nil
}

func joinComma(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
