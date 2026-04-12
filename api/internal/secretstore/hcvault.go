// SPDX-License-Identifier: AGPL-3.0-or-later
package secretstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HCVaultConfig holds the connection details for HashiCorp Vault KV v2.
type HCVaultConfig struct {
	Address   string `json:"address"`             // e.g. https://vault.example.com
	Namespace string `json:"namespace,omitempty"` // HCP Vault namespace
	// Token auth (simplest)
	Token string `json:"token,omitempty"`
	// AppRole auth (preferred for automated workloads)
	RoleID   string `json:"role_id,omitempty"`
	SecretID string `json:"secret_id,omitempty"`
	// KV v2 location
	Mount string `json:"mount"` // e.g. "secret"
	Path  string `json:"path"`  // e.g. "myapp/config"
}

// HCVaultProvider implements Provider for HashiCorp Vault KV v2.
type HCVaultProvider struct {
	cfg    HCVaultConfig
	client *http.Client
}

func (p *HCVaultProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *HCVaultProvider) FetchSecrets(ctx context.Context) (map[string]string, error) {
	if p.cfg.Address == "" {
		return nil, fmt.Errorf("hc_vault: address is required")
	}
	if p.cfg.Mount == "" || p.cfg.Path == "" {
		return nil, fmt.Errorf("hc_vault: mount and path are required")
	}

	token := p.cfg.Token
	if token == "" && p.cfg.RoleID != "" {
		var err error
		token, err = p.approleLogin(ctx)
		if err != nil {
			return nil, fmt.Errorf("hc_vault: approle login: %w", err)
		}
	}
	if token == "" {
		return nil, fmt.Errorf("hc_vault: no auth configured (set token or role_id/secret_id)")
	}

	// KV v2: GET /v1/:mount/data/:path
	addr := strings.TrimRight(p.cfg.Address, "/")
	url := addr + "/v1/" + p.cfg.Mount + "/data/" + p.cfg.Path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	if p.cfg.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", p.cfg.Namespace)
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("hc_vault: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("hc_vault: secret not found at %s/%s", p.cfg.Mount, p.cfg.Path)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hc_vault: unexpected status %d", resp.StatusCode)
	}

	// KV v2 response shape: {"data": {"data": {"KEY": "VALUE", ...}}}
	var kvResp struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&kvResp); err != nil {
		return nil, fmt.Errorf("hc_vault: decode response: %w", err)
	}

	return parseKVData(kvResp.Data.Data), nil
}

// parseKVData converts the KV v2 data map to a normalized string map.
// Non-string values are JSON-encoded so every secret becomes a valid env var.
func parseKVData(data map[string]any) map[string]string {
	result := make(map[string]string, len(data))
	for k, v := range data {
		if s, ok := v.(string); ok {
			result[normalize(k)] = s
		} else {
			encoded, _ := json.Marshal(v)
			result[normalize(k)] = string(encoded)
		}
	}
	return result
}

func (p *HCVaultProvider) approleLogin(ctx context.Context) (string, error) {
	addr := strings.TrimRight(p.cfg.Address, "/")
	url := addr + "/v1/auth/approle/login"

	body, _ := json.Marshal(map[string]string{
		"role_id":   p.cfg.RoleID,
		"secret_id": p.cfg.SecretID,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.cfg.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", p.cfg.Namespace)
	}

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("approle request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("approle login returned %d", resp.StatusCode)
	}

	var loginResp struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&loginResp); err != nil {
		return "", fmt.Errorf("approle decode: %w", err)
	}
	if loginResp.Auth.ClientToken == "" {
		return "", fmt.Errorf("approle login returned empty token")
	}
	return loginResp.Auth.ClientToken, nil
}
