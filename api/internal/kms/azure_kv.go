// SPDX-License-Identifier: AGPL-3.0-or-later
package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// AzureKVProvider implements Provider against Azure Key Vault using the
// wrapkey / unwrapkey REST operations and OAuth2 client credentials auth.
//
// The keyID must be the full key identifier URL:
//
//	https://{vault}.vault.azure.net/keys/{name}/{version}
//
// (the version segment may be omitted to use the current version).
type AzureKVProvider struct {
	keyURL       string
	tenantID     string
	clientID     string
	clientSecret string
	client       *http.Client

	tokenMu      sync.Mutex
	cachedToken  string
	tokenExpires time.Time
}

func newAzureKVProvider(keyID string) (*AzureKVProvider, error) {
	if keyID == "" || !strings.HasPrefix(keyID, "https://") {
		return nil, fmt.Errorf("azure_kv: key id must be the full key URL (https://{vault}.vault.azure.net/keys/{name}[/{version}])")
	}
	tenantID := os.Getenv("CRUCIBLE_KMS_AZURE_TENANT_ID")
	clientID := os.Getenv("CRUCIBLE_KMS_AZURE_CLIENT_ID")
	clientSecret := os.Getenv("CRUCIBLE_KMS_AZURE_CLIENT_SECRET")
	if tenantID == "" || clientID == "" || clientSecret == "" {
		return nil, fmt.Errorf("azure_kv: CRUCIBLE_KMS_AZURE_TENANT_ID / _CLIENT_ID / _CLIENT_SECRET all required")
	}
	return &AzureKVProvider{
		keyURL:       strings.TrimRight(keyID, "/"),
		tenantID:     tenantID,
		clientID:     clientID,
		clientSecret: clientSecret,
		client:       &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (p *AzureKVProvider) Wrap(ctx context.Context, plaintext []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"alg":   "RSA-OAEP-256",
		"value": base64.RawURLEncoding.EncodeToString(plaintext),
	})
	var out struct {
		Value string `json:"value"`
	}
	if err := p.call(ctx, "/wrapkey", body, &out); err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(out.Value)
}

func (p *AzureKVProvider) Unwrap(ctx context.Context, ciphertext []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"alg":   "RSA-OAEP-256",
		"value": base64.RawURLEncoding.EncodeToString(ciphertext),
	})
	var out struct {
		Value string `json:"value"`
	}
	if err := p.call(ctx, "/unwrapkey", body, &out); err != nil {
		return nil, err
	}
	return base64.RawURLEncoding.DecodeString(out.Value)
}

func (p *AzureKVProvider) TestAccess(ctx context.Context) error {
	canary := []byte("crucible-byok-test-access-canary")
	wrapped, err := p.Wrap(ctx, canary)
	if err != nil {
		return fmt.Errorf("wrap canary: %w", err)
	}
	unwrapped, err := p.Unwrap(ctx, wrapped)
	if err != nil {
		return fmt.Errorf("unwrap canary: %w", err)
	}
	if !bytes.Equal(canary, unwrapped) {
		return fmt.Errorf("azure_kv canary roundtrip mismatch")
	}
	return nil
}

// token returns a cached bearer token for the Key Vault data plane, refreshing
// it from AAD a minute before expiry.
func (p *AzureKVProvider) token(ctx context.Context) (string, error) {
	p.tokenMu.Lock()
	defer p.tokenMu.Unlock()
	if p.cachedToken != "" && time.Now().Before(p.tokenExpires.Add(-time.Minute)) {
		return p.cachedToken, nil
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", p.clientID)
	form.Set("client_secret", p.clientSecret)
	form.Set("scope", "https://vault.azure.net/.default")

	tokenURL := "https://login.microsoftonline.com/" + p.tenantID + "/oauth2/v2.0/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("aad token: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("aad token: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("aad token: decode: %w", err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("aad token: empty access_token")
	}
	p.cachedToken = out.AccessToken
	p.tokenExpires = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	return out.AccessToken, nil
}

func (p *AzureKVProvider) call(ctx context.Context, op string, body []byte, out any) error {
	tok, err := p.token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.keyURL+op+"?api-version=7.4", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("azure_kv %s: %w", op, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("azure_kv %s: status %d: %s", op, resp.StatusCode, string(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("azure_kv %s: decode response: %w", op, err)
	}
	return nil
}
