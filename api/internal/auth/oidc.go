// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible-iap/internal/audit"
	"github.com/ponack/crucible-iap/internal/config"
	"github.com/ponack/crucible-iap/internal/tokenauth"
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
	h := &Handler{cfg: cfg, pool: pool}

	if cfg.OIDCIssuerURL != "" {
		provider, err := gooidc.NewProvider(context.Background(), cfg.OIDCIssuerURL)
		if err != nil {
			slog.Error("failed to initialise OIDC provider", "issuer", cfg.OIDCIssuerURL, "err", err)
			os.Exit(1)
		}
		h.provider = provider
		h.oauth2 = &oauth2.Config{
			ClientID:     cfg.OIDCClientID,
			ClientSecret: cfg.OIDCClientSecret,
			RedirectURL:  cfg.OIDCRedirectURL,
			Endpoint:     provider.Endpoint(),
			Scopes:       []string{gooidc.ScopeOpenID, "profile", "email"},
		}
	}

	return h
}

// GetAuthConfig returns which authentication methods are available.
func (h *Handler) GetAuthConfig(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]bool{
		"oidc":  h.provider != nil,
		"local": h.cfg.LocalAuthEnabled,
	})
}

// LocalLogin authenticates a user with email + password from config.
func (h *Handler) LocalLogin(c echo.Context) error {
	if !h.cfg.LocalAuthEnabled {
		return echo.NewHTTPError(http.StatusNotFound, "local auth not enabled")
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request")
	}

	if req.Email != h.cfg.LocalAuthEmail || req.Password != h.cfg.LocalAuthPassword {
		ctx, _ := json.Marshal(map[string]string{"email": req.Email, "method": "local"})
		audit.Record(c.Request().Context(), h.pool, audit.Event{
			Action:       "auth.login.failed",
			ResourceType: "user",
			IPAddress:    c.RealIP(),
			Context:      ctx,
		})
		return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
	}

	userID, orgID, err := h.upsertUser(c.Request().Context(), req.Email, req.Email, "")
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to provision user")
	}

	return h.respondWithTokens(c, userID, orgID, req.Email, req.Email)
}

// Login redirects the user to the IdP authorization endpoint (PKCE).
func (h *Handler) Login(c echo.Context) error {
	if h.provider == nil {
		return echo.NewHTTPError(http.StatusNotFound, "OIDC not configured")
	}
	state, err := randomString(16)
	if err != nil {
		return err
	}
	nonce, err := randomString(16)
	if err != nil {
		return err
	}

	pkceVerifier, err := randomString(32)
	if err != nil {
		return err
	}

	secure := !h.cfg.IsDev()
	c.SetCookie(&http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	c.SetCookie(&http.Cookie{
		Name:     "oauth_pkce",
		Value:    pkceVerifier,
		Path:     "/",
		MaxAge:   300,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	url := h.oauth2.AuthCodeURL(state,
		gooidc.Nonce(nonce),
		oauth2.S256ChallengeOption(pkceVerifier),
	)
	return c.Redirect(http.StatusTemporaryRedirect, url)
}

// Callback handles the IdP redirect, exchanges the code, and redirects the browser
// to the frontend with short-lived tokens in the query string.
func (h *Handler) Callback(c echo.Context) error {
	if h.provider == nil {
		return echo.NewHTTPError(http.StatusNotFound, "OIDC not configured")
	}
	stateCookie, err := c.Cookie("oauth_state")
	if err != nil || stateCookie.Value != c.QueryParam("state") {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid oauth state")
	}
	c.SetCookie(&http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})

	pkceCookie, err := c.Cookie("oauth_pkce")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "missing pkce cookie")
	}
	c.SetCookie(&http.Cookie{Name: "oauth_pkce", MaxAge: -1, Path: "/"})

	code := c.QueryParam("code")
	if code == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "missing code")
	}

	token, err := h.oauth2.Exchange(c.Request().Context(), code, oauth2.VerifierOption(pkceCookie.Value))
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
		Email   string   `json:"email"`
		Name    string   `json:"name"`
		Picture string   `json:"picture"`
		Groups  []string `json:"groups"`
	}
	if err := idToken.Claims(&idClaims); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to parse claims")
	}

	userID, orgID, err := h.upsertUser(c.Request().Context(), idClaims.Email, idClaims.Name, idClaims.Picture)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to provision user")
	}
	applyGroupMappings(c.Request().Context(), h.pool, userID, idClaims.Groups)

	accessToken, err := h.issueAccessToken(userID, orgID, idClaims.Email, idClaims.Name)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue token")
	}

	refreshToken, err := h.issueRefreshToken(userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue refresh token")
	}

	h.setRefreshCookie(c, refreshToken)

	// Redirect the browser to the UI callback page with access token only.
	// The refresh token is delivered via httpOnly cookie set above.
	uiBase := h.cfg.UIBaseURL
	if uiBase == "" {
		uiBase = h.cfg.BaseURL
	}
	dest := fmt.Sprintf("%s/callback#access_token=%s", uiBase, accessToken)
	return c.Redirect(http.StatusTemporaryRedirect, dest)
}

// Refresh issues a new access token given a valid crucible_refresh httpOnly cookie.
func (h *Handler) Refresh(c echo.Context) error {
	cookie, err := c.Cookie("crucible_refresh")
	if err != nil || cookie.Value == "" {
		return echo.NewHTTPError(http.StatusUnauthorized, "refresh token required")
	}

	rc := &jwt.RegisteredClaims{}
	_, err = jwt.ParseWithClaims(cookie.Value, rc, func(t *jwt.Token) (any, error) {
		return []byte(h.cfg.SecretKey), nil
	}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience("refresh"), jwt.WithExpirationRequired())
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
	h.clearRefreshCookie(c)
	return c.NoContent(http.StatusNoContent)
}

// JWTMiddleware validates Crucible-issued access tokens and sets userID + orgID on the context.
// Accepts the token either as a Bearer header or a ?token= query param (for EventSource clients).
// Service account tokens (ciap_ prefix) are looked up in the database instead of JWT-parsed.
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

			// Service account tokens start with "ciap_" and are validated via DB lookup.
			if strings.HasPrefix(raw, "ciap_") {
				return serviceAccountAuth(raw, next, c)
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

// saFailTracker limits brute-force attempts against service account tokens.
// It tracks per-IP failure counts and locks out IPs that exceed saMaxFails
// within a saWindowSecs sliding window.
var (
	saFailMu     sync.Mutex
	saFailCounts = make(map[string][2]int64) // ip -> [count, windowResetUnix]
)

const saMaxFails = 20
const saWindowSecs = 300 // 5 minutes

func saCheckAndRecord(ip string, failed bool) bool {
	saFailMu.Lock()
	defer saFailMu.Unlock()
	now := time.Now().Unix()
	entry := saFailCounts[ip]
	if now > entry[1] {
		// Window expired — reset.
		entry = [2]int64{0, now + saWindowSecs}
	}
	if !failed {
		// Successful auth — clear the counter.
		delete(saFailCounts, ip)
		return false
	}
	entry[0]++
	saFailCounts[ip] = entry
	return entry[0] > saMaxFails
}

// serviceAccountAuth handles ciap_ prefixed tokens.
// It is set by the server via SetServiceAccountLookup before any requests are processed.
var serviceAccountAuth func(token string, next echo.HandlerFunc, c echo.Context) error = func(token string, next echo.HandlerFunc, c echo.Context) error {
	return echo.NewHTTPError(http.StatusUnauthorized, "service account auth not configured")
}

// SetServiceAccountLookup wires the DB lookup function for ciap_ tokens.
// Called once during server startup with the pgxpool.
//
// Token formats:
//   - New (argon2id): "ciap_<32hexUUID>_<43b64secret>" (81 chars total)
//     Lookup by embedded UUID; verify with argon2id.
//   - Legacy (sha256): "ciap_<43b64>" (48 chars total)
//     Lookup by SHA-256 hash for backward compat; no in-place upgrade
//     (caller must rotate to get a new-format token).
func SetServiceAccountLookup(pool *pgxpool.Pool) {
	serviceAccountAuth = func(raw string, next echo.HandlerFunc, c echo.Context) error {
		ip := c.RealIP()
		if saCheckAndRecord(ip, true) {
			return echo.NewHTTPError(http.StatusTooManyRequests, "too many failed attempts")
		}

		var id, orgID, role string
		var err error
		if len(raw) == 81 {
			id, orgID, role, err = lookupNewSAToken(c.Request().Context(), pool, raw)
		} else {
			id, orgID, role, err = lookupLegacySAToken(c.Request().Context(), pool, raw)
		}
		if err != nil {
			return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
		}

		saCheckAndRecord(ip, false)
		c.Set("userID", "sa:"+id)
		c.Set("orgID", orgID)
		c.Set("saRole", role)
		return next(c)
	}
}

// lookupNewSAToken handles "ciap_<32hexUUID>_<43b64secret>" tokens (argon2id).
func lookupNewSAToken(ctx context.Context, pool *pgxpool.Pool, raw string) (id, orgID, role string, err error) {
	body := raw[5:] // strip "ciap_"
	if len(body) != 76 || body[32] != '_' {
		return "", "", "", errors.New("malformed token")
	}
	tokenID := hexToUUID(body[:32])
	secret := body[33:]

	var storedHash, hashVersion string
	if err = pool.QueryRow(ctx, `
		SELECT id, org_id, role, token_hash, hash_version
		FROM service_account_tokens WHERE id = $1
	`, tokenID).Scan(&id, &orgID, &role, &storedHash, &hashVersion); err != nil {
		return "", "", "", err
	}

	ok, verr := tokenauth.Verify(secret, storedHash, hashVersion)
	if verr != nil || !ok {
		return "", "", "", errors.New("invalid credentials")
	}

	_, _ = pool.Exec(ctx, `UPDATE service_account_tokens SET last_used_at = now() WHERE id = $1`, id)
	return
}

// lookupLegacySAToken handles old "ciap_<43b64>" tokens (unsalted SHA-256).
// These are kept valid until the operator rotates them.
func lookupLegacySAToken(ctx context.Context, pool *pgxpool.Pool, raw string) (id, orgID, role string, err error) {
	h := sha256.Sum256([]byte(raw))
	err = pool.QueryRow(ctx, `
		UPDATE service_account_tokens
		SET last_used_at = now()
		WHERE token_hash = $1
		RETURNING id, org_id, role
	`, hex.EncodeToString(h[:])).Scan(&id, &orgID, &role)
	return
}

// hexToUUID converts a 32-char lowercase hex string to UUID format (8-4-4-4-12).
func hexToUUID(h string) string {
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
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

			// Fall back to stack token lookup: username = tokenID, password = raw secret.
			var storedHash, hashVersion, storedStackID string
			err := pool.QueryRow(c.Request().Context(), `
				SELECT token_hash, hash_version, stack_id FROM stack_tokens WHERE id = $1
			`, username).Scan(&storedHash, &hashVersion, &storedStackID)
			if err != nil || storedStackID != stackID {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="crucible"`)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
			}

			verified, _ := tokenauth.Verify(password, storedHash, hashVersion)
			if !verified {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="crucible"`)
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid credentials")
			}

			// Lazy upgrade: re-hash with argon2id on first successful use of a legacy token.
			if hashVersion == tokenauth.VersionSHA256 {
				if newHash, herr := tokenauth.Hash(password); herr == nil {
					_, _ = pool.Exec(c.Request().Context(),
						`UPDATE stack_tokens SET token_hash = $1, hash_version = 'argon2id', last_used = now() WHERE id = $2`,
						newHash, username)
				}
			} else {
				_, _ = pool.Exec(c.Request().Context(),
					`UPDATE stack_tokens SET last_used = now() WHERE id = $1`, username)
			}

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

func (h *Handler) respondWithTokens(c echo.Context, userID, orgID, email, name string) error {
	accessToken, err := h.issueAccessToken(userID, orgID, email, name)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue token")
	}
	refreshToken, err := h.issueRefreshToken(userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue refresh token")
	}
	h.setRefreshCookie(c, refreshToken)
	return c.JSON(http.StatusOK, map[string]string{
		"access_token": accessToken,
		"token_type":   "Bearer",
	})
}

// SwitchOrg issues fresh tokens scoped to a different org the user belongs to.
func (h *Handler) SwitchOrg(c echo.Context) error {
	userID := c.Get("userID").(string)

	var req struct {
		OrgID string `json:"org_id"`
	}
	if err := c.Bind(&req); err != nil || req.OrgID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "org_id required")
	}

	var email, name string
	err := h.pool.QueryRow(c.Request().Context(), `
		SELECT u.email, u.name
		FROM users u
		JOIN organization_members om ON om.user_id = u.id
		WHERE u.id = $1 AND om.org_id = $2
	`, userID, req.OrgID).Scan(&email, &name)
	if err != nil {
		return echo.NewHTTPError(http.StatusForbidden, "not a member of this org")
	}

	return h.respondWithTokens(c, userID, req.OrgID, email, name)
}

func (h *Handler) setRefreshCookie(c echo.Context, token string) {
	c.SetCookie(&http.Cookie{
		Name:     "crucible_refresh",
		Value:    token,
		Path:     "/auth",
		MaxAge:   7 * 24 * 60 * 60,
		HttpOnly: true,
		Secure:   !h.cfg.IsDev(),
		SameSite: http.SameSiteStrictMode,
	})
}

func (h *Handler) clearRefreshCookie(c echo.Context) {
	c.SetCookie(&http.Cookie{
		Name:     "crucible_refresh",
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   !h.cfg.IsDev(),
		SameSite: http.SameSiteStrictMode,
	})
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

// applyGroupMappings looks up org_sso_group_maps for each group in the IdP
// groups claim and upserts organization_members with the highest mapped role per
// org. Failures are logged and silently ignored so they never block login.
func applyGroupMappings(ctx context.Context, pool *pgxpool.Pool, userID string, groups []string) {
	if len(groups) == 0 {
		return
	}
	rows, err := pool.Query(ctx, `
		SELECT org_id, role FROM org_sso_group_maps WHERE group_claim = ANY($1)
	`, groups)
	if err != nil {
		slog.Warn("sso group mapping query failed", "err", err)
		return
	}
	defer rows.Close()

	bestRole := map[string]string{} // org_id → highest role
	for rows.Next() {
		var orgID, role string
		if err := rows.Scan(&orgID, &role); err != nil {
			continue
		}
		if existing, ok := bestRole[orgID]; !ok || roleRank(role) > roleRank(existing) {
			bestRole[orgID] = role
		}
	}
	rows.Close()

	for orgID, role := range bestRole {
		if _, err := pool.Exec(ctx, `
			INSERT INTO organization_members (org_id, user_id, role)
			VALUES ($1, $2, $3)
			ON CONFLICT (org_id, user_id) DO UPDATE SET role = EXCLUDED.role
		`, orgID, userID, role); err != nil {
			slog.Warn("sso group mapping upsert failed", "org_id", orgID, "err", err)
		}
	}
}

func roleRank(role string) int {
	switch role {
	case "admin":
		return 3
	case "member":
		return 2
	case "viewer":
		return 1
	}
	return 0
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	// RawURLEncoding (no padding) avoids truncation which would reduce effective
	// entropy. 32 bytes → 43 base64url chars, meeting RFC 7636's minimum for
	// PKCE verifiers. 16 bytes → 22 chars for state/nonce, which is fine.
	return base64.RawURLEncoding.EncodeToString(b), nil
}
