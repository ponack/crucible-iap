// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/config"
	"golang.org/x/oauth2"
)

// Claims represents the JWT claims issued by Crucible to authenticated users.
type Claims struct {
	UserID string `json:"uid"`
	OrgID  string `json:"org,omitempty"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	jwt.RegisteredClaims
}

type Handler struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	provider *gooidc.Provider
	oauth2   *oauth2.Config
}

func NewHandler(cfg *config.Config, pool *pgxpool.Pool) *Handler {
	provider, err := gooidc.NewProvider(context.Background(), cfg.OIDCIssuerURL)
	if err != nil {
		panic("failed to initialise OIDC provider: " + err.Error())
	}

	oauth2Cfg := &oauth2.Config{
		ClientID:     cfg.OIDCClientID,
		ClientSecret: cfg.OIDCClientSecret,
		RedirectURL:  cfg.OIDCRedirectURL,
		Endpoint:     provider.Endpoint(),
		Scopes:       []string{gooidc.ScopeOpenID, "profile", "email"},
	}

	return &Handler{cfg: cfg, pool: pool, provider: provider, oauth2: oauth2Cfg}
}

// Login redirects the user to the IdP authorization endpoint (PKCE).
func (h *Handler) Login(c echo.Context) error {
	state, err := randomString(16)
	if err != nil {
		return err
	}
	nonce, err := randomString(16)
	if err != nil {
		return err
	}

	c.SetCookie(&http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   !h.cfg.IsDev(),
		SameSite: http.SameSiteLaxMode,
	})

	url := h.oauth2.AuthCodeURL(state,
		gooidc.Nonce(nonce),
		oauth2.S256ChallengeOption(nonce), // PKCE
	)
	return c.Redirect(http.StatusTemporaryRedirect, url)
}

// Callback handles the IdP redirect, exchanges the code, and redirects the browser
// to the frontend with short-lived tokens in the query string.
func (h *Handler) Callback(c echo.Context) error {
	stateCookie, err := c.Cookie("oauth_state")
	if err != nil || stateCookie.Value != c.QueryParam("state") {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid oauth state")
	}
	c.SetCookie(&http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})

	code := c.QueryParam("code")
	if code == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing code")
	}

	token, err := h.oauth2.Exchange(c.Request().Context(), code)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "token exchange failed")
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return echo.NewHTTPError(http.StatusUnauthorized, "missing id_token")
	}

	verifier := h.provider.Verifier(&gooidc.Config{ClientID: h.cfg.OIDCClientID})
	idToken, err := verifier.Verify(c.Request().Context(), rawIDToken)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid id_token")
	}

	var idClaims struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idToken.Claims(&idClaims); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to parse claims")
	}

	userID, orgID, err := h.upsertUser(c.Request().Context(), idClaims.Email, idClaims.Name, idClaims.Picture)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to provision user")
	}

	accessToken, err := h.issueAccessToken(userID, orgID, idClaims.Email, idClaims.Name)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue token")
	}

	refreshToken, err := h.issueRefreshToken(userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue refresh token")
	}

	// Redirect the browser to the UI callback page with tokens.
	uiBase := h.cfg.UIBaseURL
	if uiBase == "" {
		uiBase = h.cfg.BaseURL
	}
	dest := fmt.Sprintf("%s/auth/callback?access_token=%s&refresh_token=%s",
		uiBase, accessToken, refreshToken)
	return c.Redirect(http.StatusTemporaryRedirect, dest)
}

// Refresh issues a new access token given a valid refresh token.
func (h *Handler) Refresh(c echo.Context) error {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.Bind(&req); err != nil || req.RefreshToken == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "refresh_token required")
	}

	rc := &jwt.RegisteredClaims{}
	_, err := jwt.ParseWithClaims(req.RefreshToken, rc, func(t *jwt.Token) (any, error) {
		return []byte(h.cfg.SecretKey), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience("refresh"))
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid refresh token")
	}

	userID := rc.Subject
	var email, name, orgID string
	err = h.pool.QueryRow(c.Request().Context(), `
		SELECT u.email, u.name, om.org_id
		FROM users u
		JOIN organization_members om ON om.user_id = u.id
		WHERE u.id = $1
		ORDER BY om.joined_at
		LIMIT 1
	`, userID).Scan(&email, &name, &orgID)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "user not found")
	}

	accessToken, err := h.issueAccessToken(userID, orgID, email, name)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue token")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"access_token": accessToken,
		"token_type":   "Bearer",
	})
}

func (h *Handler) Logout(c echo.Context) error {
	// Stateless JWTs — client drops tokens. Refresh tokens expire naturally.
	return c.NoContent(http.StatusNoContent)
}

// JWTMiddleware validates Crucible-issued access tokens and sets userID + orgID on the context.
// Accepts the token either as a Bearer header or a ?token= query param (for EventSource clients).
func JWTMiddleware(secretKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			raw := c.QueryParam("token")
			if raw == "" {
				header := c.Request().Header.Get("Authorization")
				if !strings.HasPrefix(header, "Bearer ") {
					return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
				}
				raw = strings.TrimPrefix(header, "Bearer ")
			}

			claims := &Claims{}
			_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
				return []byte(secretKey), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}

			c.Set("claims", claims)
			c.Set("userID", claims.UserID)
			c.Set("orgID", claims.OrgID)
			return next(c)
		}
	}
}

// BasicAuthMiddleware validates stack token credentials for the Terraform state backend.
// It accepts two credential forms:
//  1. Stack token: username=tokenID, password=rawSecret  (human/CI use)
//  2. Runner JWT:  username=stackID, password=jobToken   (ephemeral job containers)
func BasicAuthMiddleware(pool *pgxpool.Pool, secretKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			username, password, ok := c.Request().BasicAuth()
			if !ok {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="crucible"`)
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}

			stackID := c.Param("stackID")

			// Try runner JWT first (password is a JWT with aud=runner and stack_id claim).
			runnerClaims := jwt.MapClaims{}
			_, jwtErr := jwt.ParseWithClaims(password, runnerClaims, func(t *jwt.Token) (any, error) {
				return []byte(secretKey), nil
			}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience("runner"))
			if jwtErr == nil {
				claimStack, _ := runnerClaims["stack_id"].(string)
				if claimStack == stackID {
					return next(c)
				}
			}

			// Fall back to hashed stack token lookup.
			_ = username // tokenID passed as username for human tokens
			h := sha256.Sum256([]byte(password))
			hash := hex.EncodeToString(h[:])

			var storedStackID string
			err := pool.QueryRow(c.Request().Context(), `
				SELECT stack_id FROM stack_tokens
				WHERE id = $1 AND token_hash = $2
			`, username, hash).Scan(&storedStackID)
			if err != nil || storedStackID != stackID {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="crucible"`)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
			}

			_, _ = pool.Exec(c.Request().Context(),
				`UPDATE stack_tokens SET last_used = now() WHERE id = $1`, username)

			return next(c)
		}
	}
}

// RunnerAuthMiddleware validates per-job JWT tokens issued to ephemeral runner containers.
// Sets runID and stackID on the context.
func RunnerAuthMiddleware(secretKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
			}
			raw := strings.TrimPrefix(header, "Bearer ")

			claims := jwt.MapClaims{}
			_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
				return []byte(secretKey), nil
			}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience("runner"))
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid runner token")
			}

			c.Set("runID", claims["run_id"])
			c.Set("stackID", claims["stack_id"])
			return next(c)
		}
	}
}

func (h *Handler) issueAccessToken(userID, orgID, email, name string) (string, error) {
	claims := &Claims{
		UserID: userID,
		OrgID:  orgID,
		Email:  email,
		Name:   name,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			Issuer:    "crucible",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.cfg.SecretKey))
}

func (h *Handler) issueRefreshToken(userID string) (string, error) {
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(7 * 24 * time.Hour)),
		Issuer:    "crucible",
		Audience:  []string{"refresh"},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(h.cfg.SecretKey))
}

// upsertUser creates or updates the user and ensures they have a personal org.
// Returns (userID, orgID, error).
func (h *Handler) upsertUser(ctx context.Context, email, name, avatarURL string) (string, string, error) {
	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var userID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO users (email, name, avatar_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE
		  SET name        = EXCLUDED.name,
		      avatar_url  = EXCLUDED.avatar_url,
		      updated_at  = now()
		RETURNING id
	`, email, name, avatarURL).Scan(&userID); err != nil {
		return "", "", err
	}

	// Create (or find) the user's personal workspace org.
	orgSlug := "personal-" + userID[:8]
	orgName := name + "'s workspace"
	var orgID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO organizations (slug, name)
		VALUES ($1, $2)
		ON CONFLICT (slug) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, orgSlug, orgName).Scan(&orgID); err != nil {
		return "", "", err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO organization_members (org_id, user_id, role)
		VALUES ($1, $2, 'admin')
		ON CONFLICT (org_id, user_id) DO NOTHING
	`, orgID, userID); err != nil {
		return "", "", err
	}

	return userID, orgID, tx.Commit(ctx)
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b)[:n], nil
}
