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
	"os"
	"strings"
	"time"
)

// VaultTransitProvider implements Provider against HashiCorp Vault's Transit
// secrets engine. Auth is via either a static token or AppRole login.
type VaultTransitProvider struct {
	addr    string
	keyName string
	token   string // resolved at construction (either static or via AppRole)
	client  *http.Client
}

func newVaultTransitProvider(keyName string) (*VaultTransitProvider, error) {
	addr := strings.TrimRight(os.Getenv("CRUCIBLE_KMS_VAULT_ADDR"), "/")
	if addr == "" {
		return nil, fmt.Errorf("hc_vault_transit: CRUCIBLE_KMS_VAULT_ADDR is required")
	}
	if keyName == "" {
		return nil, fmt.Errorf("hc_vault_transit: key name is required")
	}

	p := &VaultTransitProvider{
		addr:    addr,
		keyName: keyName,
		client:  &http.Client{Timeout: 15 * time.Second},
	}

	token := os.Getenv("CRUCIBLE_KMS_VAULT_TOKEN")
	if token == "" {
		roleID := os.Getenv("CRUCIBLE_KMS_VAULT_ROLE_ID")
		secretID := os.Getenv("CRUCIBLE_KMS_VAULT_SECRET_ID")
		if roleID == "" || secretID == "" {
			return nil, fmt.Errorf("hc_vault_transit: set CRUCIBLE_KMS_VAULT_TOKEN, or CRUCIBLE_KMS_VAULT_ROLE_ID + _SECRET_ID")
		}
		t, err := p.appRoleLogin(context.Background(), roleID, secretID)
		if err != nil {
			return nil, err
		}
		token = t
	}
	p.token = token
	return p, nil
}

func (p *VaultTransitProvider) Wrap(ctx context.Context, plaintext []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"plaintext": base64.StdEncoding.EncodeToString(plaintext),
	})
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	if err := p.call(ctx, "/v1/transit/encrypt/"+p.keyName, body, &out); err != nil {
		return nil, err
	}
	// Vault Transit ciphertext is the string "vault:v1:<base64>" — we store the
	// whole prefixed form so Vault can route it to the correct key version.
	return []byte(out.Data.Ciphertext), nil
}

func (p *VaultTransitProvider) Unwrap(ctx context.Context, ciphertext []byte) ([]byte, error) {
	body, _ := json.Marshal(map[string]string{
		"ciphertext": string(ciphertext),
	})
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := p.call(ctx, "/v1/transit/decrypt/"+p.keyName, body, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.Data.Plaintext)
}

func (p *VaultTransitProvider) TestAccess(ctx context.Context) error {
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
		return fmt.Errorf("vault transit canary roundtrip mismatch")
	}
	return nil
}

func (p *VaultTransitProvider) appRoleLogin(ctx context.Context, roleID, secretID string) (string, error) {
	body, _ := json.Marshal(map[string]string{"role_id": roleID, "secret_id": secretID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.addr+"/v1/auth/approle/login", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("approle login: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("approle login: status %d: %s", resp.StatusCode, string(raw))
	}
	var out struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("approle login: decode: %w", err)
	}
	if out.Auth.ClientToken == "" {
		return "", fmt.Errorf("approle login: empty client_token")
	}
	return out.Auth.ClientToken, nil
}

func (p *VaultTransitProvider) call(ctx context.Context, path string, body []byte, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.addr+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Vault-Token", p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("vault transit %s: %w", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("vault transit %s: status %d: %s", path, resp.StatusCode, string(raw))
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("vault transit %s: decode response: %w", path, err)
	}
	return nil
}
