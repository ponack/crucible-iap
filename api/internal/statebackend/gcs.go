// SPDX-License-Identifier: AGPL-3.0-or-later
package statebackend

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// GCSConfig holds credentials and bucket info for Google Cloud Storage state.
// State is stored at gs://{bucket}/{key_prefix}{stackID}/terraform.tfstate
//
// ServiceAccountJSON should be the full content of a GCP service account key
// file (the JSON downloaded from the Cloud Console). The account needs the
// Storage Object Admin role on the target bucket.
type GCSConfig struct {
	Bucket             string `json:"bucket"`
	KeyPrefix          string `json:"key_prefix,omitempty"`
	ServiceAccountJSON string `json:"service_account_json"` // full JSON key file
}

type GCSBackend struct {
	cfg    GCSConfig
	client *http.Client
}

func (b *GCSBackend) httpClient() *http.Client {
	if b.client != nil {
		return b.client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (b *GCSBackend) objectName(stackID string) string {
	return url.PathEscape(b.cfg.KeyPrefix + stackID + "/terraform.tfstate")
}

func (b *GCSBackend) objectURL(stackID string) string {
	return "https://storage.googleapis.com/storage/v1/b/" +
		b.cfg.Bucket + "/o/" + b.objectName(stackID)
}

func (b *GCSBackend) uploadURL(stackID string) string {
	return "https://storage.googleapis.com/upload/storage/v1/b/" +
		b.cfg.Bucket + "/o?uploadType=media&name=" +
		url.QueryEscape(b.cfg.KeyPrefix+stackID+"/terraform.tfstate")
}

func (b *GCSBackend) GetState(ctx context.Context, stackID string) (io.ReadCloser, error) {
	token, err := b.accessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("gcs auth: %w", err)
	}

	// alt=media returns the object content directly.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		b.objectURL(stackID)+"?alt=media", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("gcs get: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (b *GCSBackend) PutState(ctx context.Context, stackID string, data []byte) error {
	token, err := b.accessToken(ctx)
	if err != nil {
		return fmt.Errorf("gcs auth: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		b.uploadURL(stackID), bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("gcs put: status %d", resp.StatusCode)
	}
	return nil
}

func (b *GCSBackend) DeleteState(ctx context.Context, stackID string) error {
	token, err := b.accessToken(ctx)
	if err != nil {
		return fmt.Errorf("gcs auth: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, b.objectURL(stackID), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gcs delete: status %d", resp.StatusCode)
	}
	return nil
}

// accessToken exchanges the service account key for a short-lived OAuth2 bearer token.
func (b *GCSBackend) accessToken(ctx context.Context) (string, error) {
	var sa struct {
		ClientEmail string `json:"client_email"`
		PrivateKey  string `json:"private_key"`
		TokenURI    string `json:"token_uri"`
	}
	if err := json.Unmarshal([]byte(b.cfg.ServiceAccountJSON), &sa); err != nil {
		return "", fmt.Errorf("parse service account JSON: %w", err)
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}

	// Parse RSA private key.
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", fmt.Errorf("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return "", fmt.Errorf("private key is not RSA")
	}

	// Build and sign a JWT.
	now := time.Now().Unix()
	claimsJSON, _ := json.Marshal(map[string]any{
		"iss":   sa.ClientEmail,
		"scope": "https://www.googleapis.com/auth/devstorage.read_write",
		"aud":   sa.TokenURI,
		"iat":   now,
		"exp":   now + 3600,
	})
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims := base64.RawURLEncoding.EncodeToString(claimsJSON)
	sigInput := header + "." + claims

	h := sha256.Sum256([]byte(sigInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, h[:])
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}
	jwt := sigInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	// Exchange JWT for access token.
	form := "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Ajwt-bearer&assertion=" + url.QueryEscape(jwt)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sa.TokenURI,
		strings.NewReader(form))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tok); err != nil {
		return "", err
	}
	if tok.AccessToken == "" {
		return "", fmt.Errorf("empty access token in response")
	}
	return tok.AccessToken, nil
}
