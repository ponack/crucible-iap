// SPDX-License-Identifier: AGPL-3.0-or-later
// Package githubapp manages the org-level GitHub App: registration, JWT
// minting, installation-token caching, and the helper that other packages
// (notify, webhooks, runs) use to make authenticated GitHub API calls.
//
// One GitHub App is registered per Crucible org. The app is created on
// github.com manually by an admin; the resulting App ID, private key,
// client credentials, and webhook secret are pasted into Crucible.
//
// Authentication flow:
//  1. JWT signed with the app's private key (RS256, max 10-minute validity)
//  2. Exchange the JWT for an installation access token (1-hour validity)
//     via POST /app/installations/{id}/access_tokens
//  3. Use the installation token as `Authorization: token <token>` for
//     PR comments, commit statuses, and repo reads.
//
// Installation tokens are cached in memory keyed by installation_id; the
// cache refreshes ~5 minutes before expiry to avoid races.
package githubapp

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// vaultContext returns the HKDF context string used to encrypt/decrypt this
// app's secrets. Using the app UUID scopes the key uniquely.
func vaultContext(appUUID string) string {
	return "crucible-githubapp:" + appUUID
}

// App holds the metadata returned by the API. Secret fields are never returned.
type App struct {
	ID        string    `json:"id"`
	AppID     int64     `json:"app_id"`
	Slug      string    `json:"slug"`
	Name      string    `json:"name"`
	ClientID  string    `json:"client_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// MintJWT generates an RS256-signed JWT valid for ~10 minutes that authenticates
// as the GitHub App itself. Used to call /app/installations/{id}/access_tokens.
//
// privatePEM is the contents of the PEM file GitHub provides on app creation;
// appID is the numeric app id GitHub assigns.
func MintJWT(privatePEM []byte, appID int64) (string, error) {
	key, err := parseRSAKey(privatePEM)
	if err != nil {
		return "", err
	}
	now := time.Now()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		// 60s back-dated to absorb minor clock skew between us and github.com.
		"iat": now.Add(-60 * time.Second).Unix(),
		"exp": now.Add(9 * time.Minute).Unix(),
		"iss": appID,
	})
	return tok.SignedString(key)
}

func parseRSAKey(pemBytes []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("private key is not valid PEM")
	}
	// GitHub issues PKCS#1 keys; some tooling re-exports as PKCS#8. Try both.
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return k, nil
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("private key is not RSA")
		}
		return rsaKey, nil
	}
	return nil, errors.New("private key is neither PKCS#1 nor PKCS#8 RSA")
}

// cachedToken is one entry in the per-installation token cache.
type cachedToken struct {
	token     string
	expiresAt time.Time
}

// TokenCache memoises installation access tokens until shortly before they expire.
// Safe for concurrent use.
type TokenCache struct {
	mu      sync.Mutex
	entries map[int64]cachedToken
	// Refresh tokens this far before their actual expiry to avoid a request
	// landing on an expired token under clock skew.
	refreshSkew time.Duration
}

func NewTokenCache() *TokenCache {
	return &TokenCache{
		entries:     make(map[int64]cachedToken),
		refreshSkew: 5 * time.Minute,
	}
}

// Lookup returns a cached token if still valid, plus a bool indicating a hit.
func (c *TokenCache) Lookup(installationID int64) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[installationID]
	if !ok {
		return "", false
	}
	if time.Now().Add(c.refreshSkew).After(e.expiresAt) {
		return "", false
	}
	return e.token, true
}

// Store records a token and its expiry under the given installation_id.
func (c *TokenCache) Store(installationID int64, token string, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[installationID] = cachedToken{token: token, expiresAt: expiresAt}
}

// VaultContext is the public-facing context-key helper for callers that need
// to encrypt / decrypt this app's secrets directly. The handler uses this; the
// next PR's installation-token service will use it as well.
func VaultContext(appUUID string) string {
	return vaultContext(appUUID)
}
