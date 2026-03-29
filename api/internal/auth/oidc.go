// SPDX-License-Identifier: AGPL-3.0-or-later
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/ponack/crucible/internal/config"
	"golang.org/x/oauth2"
)

// Claims represents the JWT claims issued by Crucible to authenticated users.
type Claims struct {
	UserID string `json:"uid"`
	Email  string `json:"email"`
	Name   string `json:"name"`
	OrgID  string `json:"org,omitempty"`
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

	// Store state + nonce in a short-lived cookie for CSRF validation
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

// Callback handles the IdP redirect, exchanges the code, and issues Crucible JWTs.
func (h *Handler) Callback(c echo.Context) error {
	stateCookie, err := c.Cookie("oauth_state")
	if err != nil || stateCookie.Value != c.QueryParam("state") {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid oauth state")
	}

	// Clear the state cookie
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

	var claims struct {
		Email   string `json:"email"`
		Name    string `json:"name"`
		Picture string `json:"picture"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to parse claims")
	}

	// Upsert user in database
	userID, err := h.upsertUser(c.Request().Context(), claims.Email, claims.Name, claims.Picture)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to provision user")
	}

	accessToken, err := h.issueAccessToken(userID, claims.Email, claims.Name)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue token")
	}

	refreshToken, err := h.issueRefreshToken(userID)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to issue refresh token")
	}

	return c.JSON(http.StatusOK, map[string]string{
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"token_type":    "Bearer",
	})
}

func (h *Handler) Refresh(c echo.Context) error {
	// TODO: validate refresh token, issue new access token
	return echo.NewHTTPError(http.StatusNotImplemented, "coming soon")
}

func (h *Handler) Logout(c echo.Context) error {
	// TODO: invalidate refresh token
	return c.NoContent(http.StatusNoContent)
}

// JWTMiddleware validates Crucible-issued access tokens.
func JWTMiddleware(secretKey string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			header := c.Request().Header.Get("Authorization")
			if !strings.HasPrefix(header, "Bearer ") {
				return echo.NewHTTPError(http.StatusUnauthorized, "missing bearer token")
			}
			raw := strings.TrimPrefix(header, "Bearer ")

			claims := &Claims{}
			_, err := jwt.ParseWithClaims(raw, claims, func(t *jwt.Token) (any, error) {
				return []byte(secretKey), nil
			}, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil {
				return echo.NewHTTPError(http.StatusUnauthorized, "invalid token")
			}

			c.Set("claims", claims)
			c.Set("userID", claims.UserID)
			return next(c)
		}
	}
}

// BasicAuthMiddleware validates HTTP Basic auth for the Terraform state backend.
func BasicAuthMiddleware(pool *pgxpool.Pool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			username, password, ok := c.Request().BasicAuth()
			if !ok {
				c.Response().Header().Set("WWW-Authenticate", `Basic realm="crucible"`)
				return echo.NewHTTPError(http.StatusUnauthorized, "authentication required")
			}
			// TODO: validate username (stack token ID) + password (token secret hash)
			_ = username
			_ = password
			_ = pool
			return next(c)
		}
	}
}

func (h *Handler) issueAccessToken(userID, email, name string) (string, error) {
	claims := &Claims{
		UserID: userID,
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

func (h *Handler) upsertUser(ctx context.Context, email, name, avatarURL string) (string, error) {
	var id string
	err := h.pool.QueryRow(ctx, `
		INSERT INTO users (email, name, avatar_url)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE
		  SET name = EXCLUDED.name,
		      avatar_url = EXCLUDED.avatar_url,
		      updated_at = now()
		RETURNING id
	`, email, name, avatarURL).Scan(&id)
	return id, err
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b)[:n], nil
}
