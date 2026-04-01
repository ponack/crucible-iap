// SPDX-License-Identifier: AGPL-3.0-or-later
package secretstore

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BitwardenConfig holds credentials for the Bitwarden Secrets Manager.
// The access token is the machine account token issued by Bitwarden SM.
// It must be in the format: 0.<serviceAccountId>.<clientSecret>.<base64EncKey>
// where the encryption key is a 64-byte symmetric key (32 bytes AES-256 + 32 bytes HMAC-SHA256).
type BitwardenConfig struct {
	AccessToken string `json:"access_token"`
	// ProjectID filters secrets to a specific SM project. Required unless OrgID is set.
	ProjectID string `json:"project_id,omitempty"`
	// OrgID fetches all org secrets when no ProjectID is set.
	OrgID string `json:"org_id,omitempty"`
	// Optional overrides for self-hosted Bitwarden deployments.
	APIURL      string `json:"api_url,omitempty"`
	IdentityURL string `json:"identity_url,omitempty"`
}

// BitwardenProvider implements Provider for Bitwarden Secrets Manager.
type BitwardenProvider struct {
	cfg    BitwardenConfig
	client *http.Client
}

func (p *BitwardenProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *BitwardenProvider) FetchSecrets(ctx context.Context) (map[string]string, error) {
	if p.cfg.AccessToken == "" {
		return nil, fmt.Errorf("bitwarden_sm: access_token is required")
	}
	if p.cfg.ProjectID == "" && p.cfg.OrgID == "" {
		return nil, fmt.Errorf("bitwarden_sm: project_id or org_id is required")
	}

	// Parse the structured access token: "0.<serviceAccountId>.<clientSecret>.<encKey>"
	parts := strings.SplitN(p.cfg.AccessToken, ".", 4)
	if len(parts) != 4 || parts[0] != "0" {
		return nil, fmt.Errorf("bitwarden_sm: invalid access token format (expected 0.<id>.<secret>.<key>)")
	}
	serviceAccountID := parts[1]
	clientSecret := parts[2]
	encKeyB64 := parts[3]

	// Decode symmetric encryption key (64 bytes: 32 AES + 32 HMAC)
	encKeyBytes, err := base64.StdEncoding.DecodeString(encKeyB64)
	if err != nil {
		// Try RawURLEncoding (some tokens omit padding)
		encKeyBytes, err = base64.RawURLEncoding.DecodeString(encKeyB64)
		if err != nil {
			return nil, fmt.Errorf("bitwarden_sm: decode encryption key: %w", err)
		}
	}
	if len(encKeyBytes) != 64 {
		return nil, fmt.Errorf("bitwarden_sm: encryption key must be 64 bytes, got %d", len(encKeyBytes))
	}
	aesKey := encKeyBytes[:32]
	macKey := encKeyBytes[32:]

	apiURL := "https://api.bitwarden.com"
	if p.cfg.APIURL != "" {
		apiURL = strings.TrimRight(p.cfg.APIURL, "/")
	}
	identityURL := "https://identity.bitwarden.com"
	if p.cfg.IdentityURL != "" {
		identityURL = strings.TrimRight(p.cfg.IdentityURL, "/")
	}

	// Exchange machine account credentials for a bearer token.
	bearer, err := p.authenticate(ctx, identityURL, serviceAccountID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("bitwarden_sm: auth: %w", err)
	}

	// List secret IDs for the configured project or org.
	secretIDs, err := p.listSecretIDs(ctx, apiURL, bearer)
	if err != nil {
		return nil, fmt.Errorf("bitwarden_sm: list secrets: %w", err)
	}
	if len(secretIDs) == 0 {
		return nil, nil
	}

	// Batch-fetch secrets by ID.
	rawSecrets, err := p.getSecretsByIDs(ctx, apiURL, bearer, secretIDs)
	if err != nil {
		return nil, fmt.Errorf("bitwarden_sm: get secrets: %w", err)
	}

	// Decrypt key and value for each secret.
	result := make(map[string]string, len(rawSecrets))
	for _, s := range rawSecrets {
		name, err := bwDecrypt(s.Key, aesKey, macKey)
		if err != nil {
			return nil, fmt.Errorf("bitwarden_sm: decrypt secret name: %w", err)
		}
		value, err := bwDecrypt(s.Value, aesKey, macKey)
		if err != nil {
			return nil, fmt.Errorf("bitwarden_sm: decrypt secret value: %w", err)
		}
		result[normalize(string(name))] = string(value)
	}
	return result, nil
}

func (p *BitwardenProvider) authenticate(ctx context.Context, identityURL, serviceAccountID, clientSecret string) (string, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"scope":         {"api.secrets"},
		"client_id":     {"sm-access-token." + serviceAccountID},
		"client_secret": {clientSecret},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		identityURL+"/connect/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("identity server returned %d", resp.StatusCode)
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tokenResp.AccessToken == "" {
		return "", fmt.Errorf("empty bearer token in response")
	}
	return tokenResp.AccessToken, nil
}

func (p *BitwardenProvider) listSecretIDs(ctx context.Context, apiURL, bearer string) ([]string, error) {
	var listURL string
	if p.cfg.ProjectID != "" {
		listURL = apiURL + "/smapi/projects/" + p.cfg.ProjectID + "/secrets"
	} else {
		listURL = apiURL + "/smapi/organizations/" + p.cfg.OrgID + "/secrets"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list endpoint returned %d", resp.StatusCode)
	}

	var listResp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(listResp.Data))
	for _, s := range listResp.Data {
		if s.ID != "" {
			ids = append(ids, s.ID)
		}
	}
	return ids, nil
}

type bwRawSecret struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

func (p *BitwardenProvider) getSecretsByIDs(ctx context.Context, apiURL, bearer string, ids []string) ([]bwRawSecret, error) {
	body, _ := json.Marshal(map[string][]string{"ids": ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiURL+"/smapi/secrets/get-by-ids",
		bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("get-by-ids returned %d: %s", resp.StatusCode, b)
	}

	var batchResp struct {
		Data []bwRawSecret `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&batchResp); err != nil {
		return nil, err
	}
	return batchResp.Data, nil
}

// bwDecrypt decrypts a Bitwarden AesCbc256_HmacSha256_B64 encrypted string.
// Format: "2.<iv_base64>|<ciphertext_base64>|<mac_base64>"
func bwDecrypt(enc string, aesKey, macKey []byte) ([]byte, error) {
	// Strip the type prefix ("2." = AesCbc256_HmacSha256_B64)
	if !strings.HasPrefix(enc, "2.") {
		return nil, fmt.Errorf("unsupported encryption type in %q (expected type 2)", enc[:min(4, len(enc))])
	}
	enc = enc[2:]

	parts := strings.SplitN(enc, "|", 3)
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid encrypted string: expected 3 pipe-separated parts")
	}

	iv, err := base64.StdEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode iv: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode ciphertext: %w", err)
	}
	mac, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode mac: %w", err)
	}

	// Verify HMAC-SHA256(macKey, iv || ciphertext)
	h := hmac.New(sha256.New, macKey)
	h.Write(iv)
	h.Write(ct)
	if !hmac.Equal(h.Sum(nil), mac) {
		return nil, fmt.Errorf("HMAC verification failed — wrong encryption key or corrupted data")
	}

	// AES-256-CBC decrypt
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, err
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("ciphertext length %d not a multiple of block size", len(ct))
	}
	plaintext := make([]byte, len(ct))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ct)

	return pkcs7Unpad(plaintext)
}

// pkcs7Unpad removes PKCS#7 padding from decrypted data.
func pkcs7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty plaintext after decryption")
	}
	pad := int(data[len(data)-1])
	if pad == 0 || pad > aes.BlockSize || pad > len(data) {
		return nil, fmt.Errorf("invalid PKCS7 padding byte: %d", pad)
	}
	return data[:len(data)-pad], nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
