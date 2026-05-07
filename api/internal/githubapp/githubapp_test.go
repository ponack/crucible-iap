// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func generateTestKey(t *testing.T) (*rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	return key, pemBytes
}

func TestMintJWT_PKCS1(t *testing.T) {
	key, pemBytes := generateTestKey(t)

	tokenStr, err := MintJWT(pemBytes, 12345)
	if err != nil {
		t.Fatalf("MintJWT: %v", err)
	}

	parsed, err := jwt.Parse(tokenStr, func(_ *jwt.Token) (any, error) {
		return &key.PublicKey, nil
	})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims wrong type: %T", parsed.Claims)
	}
	iss, ok := claims["iss"].(float64)
	if !ok || int64(iss) != 12345 {
		t.Errorf("iss = %v, want 12345", claims["iss"])
	}
	if parsed.Method.Alg() != "RS256" {
		t.Errorf("alg = %s, want RS256", parsed.Method.Alg())
	}
}

func TestMintJWT_PKCS8(t *testing.T) {
	key, _ := generateTestKey(t)
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pkcs8})

	if _, err := MintJWT(pemBytes, 99); err != nil {
		t.Fatalf("MintJWT (PKCS8): %v", err)
	}
}

func TestMintJWT_InvalidPEM(t *testing.T) {
	if _, err := MintJWT([]byte("not a pem"), 1); err == nil {
		t.Error("expected error on invalid PEM")
	}
}

func TestMintJWT_ClaimWindow(t *testing.T) {
	_, pemBytes := generateTestKey(t)
	tokenStr, err := MintJWT(pemBytes, 1)
	if err != nil {
		t.Fatalf("MintJWT: %v", err)
	}
	parser := jwt.NewParser(jwt.WithoutClaimsValidation())
	parsed, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	claims := parsed.Claims.(jwt.MapClaims)
	now := time.Now().Unix()
	iat := int64(claims["iat"].(float64))
	exp := int64(claims["exp"].(float64))
	if iat > now || iat < now-120 {
		t.Errorf("iat %d not in expected back-dated window of now=%d", iat, now)
	}
	// 9 minutes is within GitHub's 10-minute cap
	if exp-now < 8*60 || exp-now > 10*60 {
		t.Errorf("exp window %ds, want ~9 minutes", exp-now)
	}
}

func TestTokenCache_HitAndExpiry(t *testing.T) {
	c := NewTokenCache()

	if _, ok := c.Lookup(1); ok {
		t.Error("empty cache returned a hit")
	}

	c.Store(1, "token-a", time.Now().Add(30*time.Minute))
	got, ok := c.Lookup(1)
	if !ok || got != "token-a" {
		t.Errorf("Lookup() = (%q, %v), want (token-a, true)", got, ok)
	}

	// Within refresh-skew window — should miss to force refresh.
	c.Store(2, "token-b", time.Now().Add(2*time.Minute))
	if _, ok := c.Lookup(2); ok {
		t.Error("token within refresh-skew window should be treated as a miss")
	}
}

func TestTokenCache_DistinctInstallations(t *testing.T) {
	c := NewTokenCache()
	exp := time.Now().Add(time.Hour)
	c.Store(1, "a", exp)
	c.Store(2, "b", exp)

	if v, _ := c.Lookup(1); v != "a" {
		t.Errorf("install 1 = %q, want a", v)
	}
	if v, _ := c.Lookup(2); v != "b" {
		t.Errorf("install 2 = %q, want b", v)
	}
}
