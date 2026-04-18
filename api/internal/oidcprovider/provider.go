// SPDX-License-Identifier: AGPL-3.0-or-later
// Package oidcprovider implements a minimal OIDC identity provider for workload
// identity federation. It issues short-lived JWTs signed with an ECDSA P-256 key
// that AWS, GCP, and Azure can exchange for cloud credentials without storing
// long-lived static secrets in Crucible.
package oidcprovider

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ponack/crucible-iap/internal/vault"
)

// Provider holds the OIDC signing key and issues JWTs.
type Provider struct {
	privateKey *ecdsa.PrivateKey
	kid        string // RFC 7638 SHA-256 thumbprint used as key ID
	issuer     string // Crucible base URL
}

// TokenClaims carries all context needed for workload identity federation.
type TokenClaims struct {
	jwt.RegisteredClaims
	StackID   string `json:"stack_id"`
	StackSlug string `json:"stack_slug"`
	OrgID     string `json:"org_id"`
	RunID     string `json:"run_id"`
	RunType   string `json:"run_type"`
	Branch    string `json:"branch"`
	Trigger   string `json:"trigger"`
}

// LoadOrCreate loads the ECDSA P-256 signing key from the database (encrypted by
// the vault) or generates and stores a new one on first call.
func LoadOrCreate(ctx context.Context, pool *pgxpool.Pool, v *vault.Vault, issuer string) (*Provider, error) {
	// Ensure the singleton row exists (guards against restored DBs missing the seed).
	_, _ = pool.Exec(ctx, `INSERT INTO system_settings DEFAULT VALUES ON CONFLICT DO NOTHING`)

	var enc []byte
	err := pool.QueryRow(ctx,
		`SELECT oidc_signing_key_enc FROM system_settings LIMIT 1`,
	).Scan(&enc)
	if err != nil {
		return nil, fmt.Errorf("oidc: read signing key: %w", err)
	}

	var key *ecdsa.PrivateKey
	if len(enc) > 0 {
		pemBytes, err := v.DecryptFor("oidc-signing-key", enc)
		if err != nil {
			return nil, fmt.Errorf("oidc: decrypt signing key: %w", err)
		}
		key, err = parseECPrivateKeyPEM(pemBytes)
		if err != nil {
			return nil, fmt.Errorf("oidc: parse signing key: %w", err)
		}
	} else {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, fmt.Errorf("oidc: generate signing key: %w", err)
		}
		pemBytes, err := marshalECPrivateKeyPEM(key)
		if err != nil {
			return nil, fmt.Errorf("oidc: marshal signing key: %w", err)
		}
		encBytes, err := v.EncryptFor("oidc-signing-key", pemBytes)
		if err != nil {
			return nil, fmt.Errorf("oidc: encrypt signing key: %w", err)
		}
		_, err = pool.Exec(ctx,
			`UPDATE system_settings SET oidc_signing_key_enc = $1`, encBytes)
		if err != nil {
			return nil, fmt.Errorf("oidc: store signing key: %w", err)
		}
	}

	kid, err := keyThumbprint(key)
	if err != nil {
		return nil, fmt.Errorf("oidc: thumbprint: %w", err)
	}

	return &Provider{privateKey: key, kid: kid, issuer: issuer}, nil
}

// IssueToken mints a signed JWT for the given run context. ttl is typically 1 hour.
func (p *Provider) IssueToken(claims TokenClaims, ttl time.Duration) (string, error) {
	now := time.Now()
	claims.RegisteredClaims = jwt.RegisteredClaims{
		Issuer:    p.issuer,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	t.Header["kid"] = p.kid
	return t.SignedString(p.privateKey)
}

// Issuer returns the OIDC issuer URL (Crucible base URL).
func (p *Provider) Issuer() string { return p.issuer }

// JWKS returns the JSON Web Key Set for the current signing key.
func (p *Provider) JWKS() (json.RawMessage, error) {
	pub := p.privateKey.PublicKey
	x := base64.RawURLEncoding.EncodeToString(pub.X.Bytes())
	y := base64.RawURLEncoding.EncodeToString(pub.Y.Bytes())

	key := map[string]any{
		"kty": "EC",
		"crv": "P-256",
		"use": "sig",
		"alg": "ES256",
		"kid": p.kid,
		"x":   x,
		"y":   y,
	}
	return json.Marshal(map[string]any{"keys": []any{key}})
}

// keyThumbprint computes the RFC 7638 SHA-256 thumbprint for an EC public key.
func keyThumbprint(key *ecdsa.PrivateKey) (string, error) {
	pub := key.PublicKey
	// Members must be in lexicographic order: crv, kty, x, y
	thumbprintJSON := fmt.Sprintf(
		`{"crv":"P-256","kty":"EC","x":"%s","y":"%s"}`,
		base64.RawURLEncoding.EncodeToString(zeroPad(pub.X, 32)),
		base64.RawURLEncoding.EncodeToString(zeroPad(pub.Y, 32)),
	)
	h := sha256.Sum256([]byte(thumbprintJSON))
	return base64.RawURLEncoding.EncodeToString(h[:]), nil
}

// zeroPad ensures an EC coordinate big.Int is exactly n bytes (big-endian).
func zeroPad(n *big.Int, size int) []byte {
	b := n.Bytes()
	if len(b) >= size {
		return b
	}
	padded := make([]byte, size)
	copy(padded[size-len(b):], b)
	return padded
}

func parseECPrivateKeyPEM(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	return x509.ParseECPrivateKey(block.Bytes)
}

func marshalECPrivateKeyPEM(key *ecdsa.PrivateKey) ([]byte, error) {
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}), nil
}
