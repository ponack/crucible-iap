// SPDX-License-Identifier: AGPL-3.0-or-later
package secretstore

import (
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

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/pbkdf2"
)

// VaultwardenConfig holds credentials for a Vaultwarden (or vanilla Bitwarden)
// self-hosted password vault. Secrets are stored as SecureNote items in a
// specified folder; each item's decrypted name becomes the env var key and its
// decrypted notes field becomes the value.
//
// Authentication uses the Bitwarden API key flow (client_id + client_secret
// obtained from Account Settings → Security → Keys → API key) plus the master
// password which is required to decrypt the vault's symmetric key.
type VaultwardenConfig struct {
	URL            string `json:"url"`             // e.g. https://vault.example.com
	ClientID       string `json:"client_id"`       // user.{uuid}
	ClientSecret   string `json:"client_secret"`   // from account API key settings
	Email          string `json:"email"`           // account email (needed for key derivation)
	MasterPassword string `json:"master_password"` // account master password
	// FolderName filters to SecureNote items in a specific folder.
	// Leave empty to fetch all SecureNote items.
	FolderName string `json:"folder_name,omitempty"`
}

// VaultwardenProvider implements Provider for Vaultwarden / self-hosted Bitwarden.
type VaultwardenProvider struct {
	cfg    VaultwardenConfig
	client *http.Client
}

func (p *VaultwardenProvider) httpClient() *http.Client {
	if p.client != nil {
		return p.client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (p *VaultwardenProvider) FetchSecrets(ctx context.Context) (map[string]string, error) {
	if p.cfg.URL == "" || p.cfg.ClientID == "" || p.cfg.ClientSecret == "" ||
		p.cfg.Email == "" || p.cfg.MasterPassword == "" {
		return nil, fmt.Errorf("vaultwarden: url, client_id, client_secret, email, and master_password are all required")
	}

	base := strings.TrimRight(p.cfg.URL, "/")

	// ── Step 1: Authenticate and get login response ──────────────────────────
	loginResp, err := p.login(ctx, base)
	if err != nil {
		return nil, fmt.Errorf("vaultwarden: login: %w", err)
	}

	// ── Step 2: Derive account symmetric key from master password ─────────────
	symKey, err := p.deriveSymKey(loginResp)
	if err != nil {
		return nil, fmt.Errorf("vaultwarden: key derivation: %w", err)
	}

	// ── Step 3: Fetch ciphers ─────────────────────────────────────────────────
	ciphers, err := p.fetchCiphers(ctx, base, loginResp.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("vaultwarden: fetch ciphers: %w", err)
	}

	// ── Step 4: Optionally resolve folder ID ─────────────────────────────────
	var folderID string
	if p.cfg.FolderName != "" {
		folderID, err = p.resolveFolderID(ctx, base, loginResp.AccessToken, symKey, p.cfg.FolderName)
		if err != nil {
			return nil, fmt.Errorf("vaultwarden: resolve folder: %w", err)
		}
	}

	// ── Step 5: Decrypt matching SecureNote items ─────────────────────────────
	result := make(map[string]string)
	for _, c := range ciphers {
		if c.Type != 2 { // 2 = SecureNote
			continue
		}
		if folderID != "" && c.FolderID != folderID {
			continue
		}

		name, err := vwDecrypt(c.Name, symKey[:32], symKey[32:])
		if err != nil {
			continue // skip un-decryptable items
		}
		notes, err := vwDecrypt(c.Notes, symKey[:32], symKey[32:])
		if err != nil {
			continue
		}
		result[normalize(string(name))] = string(notes)
	}
	return result, nil
}

// ── Login ─────────────────────────────────────────────────────────────────────

type vwLoginResp struct {
	AccessToken string `json:"access_token"`
	Key         string `json:"key"` // protected symmetric key
	// KDF params
	Kdf            int `json:"Kdf"` // 0=PBKDF2, 1=Argon2id
	KdfIterations  int `json:"KdfIterations"`
	KdfMemory      int `json:"KdfMemory"`      // Argon2id only, in MB
	KdfParallelism int `json:"KdfParallelism"` // Argon2id only
	// Profile contains the email (used for KDF)
	PrivateKey string `json:"PrivateKey"`
}

func (p *VaultwardenProvider) login(ctx context.Context, base string) (*vwLoginResp, error) {
	form := url.Values{
		"grant_type":       {"client_credentials"},
		"scope":            {"api"},
		"client_id":        {p.cfg.ClientID},
		"client_secret":    {p.cfg.ClientSecret},
		"deviceType":       {"21"}, // SDK
		"deviceName":       {"crucible-iap"},
		"deviceIdentifier": {"crucible-iap-runner"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		base+"/identity/connect/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		var e struct {
			ErrorDescription string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, e.ErrorDescription)
	}

	var lr vwLoginResp
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, err
	}
	if lr.AccessToken == "" {
		return nil, fmt.Errorf("empty access token")
	}
	return &lr, nil
}

// ── Key derivation ────────────────────────────────────────────────────────────

// deriveSymKey decrypts the account's protected symmetric key using the master
// password. Returns a 64-byte key: [0:32] = AES enc key, [32:64] = HMAC key.
func (p *VaultwardenProvider) deriveSymKey(lr *vwLoginResp) ([]byte, error) {
	if lr.KdfIterations == 0 {
		lr.KdfIterations = 100_000 // Bitwarden default
	}

	var masterKey []byte
	switch lr.Kdf {
	case 0: // PBKDF2-SHA256
		masterKey = pbkdf2.Key(
			[]byte(p.cfg.MasterPassword),
			[]byte(strings.ToLower(p.cfg.Email)),
			lr.KdfIterations, 32, sha256.New)
	case 1: // Argon2id
		mem := uint32(64)
		if lr.KdfMemory > 0 {
			mem = uint32(lr.KdfMemory)
		}
		par := uint8(4)
		if lr.KdfParallelism > 0 {
			par = uint8(lr.KdfParallelism)
		}
		masterKey = argon2.IDKey(
			[]byte(p.cfg.MasterPassword),
			[]byte(strings.ToLower(p.cfg.Email)),
			uint32(lr.KdfIterations), mem*1024, par, 32)
	default:
		return nil, fmt.Errorf("unsupported KDF type %d", lr.Kdf)
	}

	// Stretch master key → 64-byte account key using HKDF-like expand.
	encKey := vwHKDFExpand(masterKey, []byte("enc"), 32)
	macKey := vwHKDFExpand(masterKey, []byte("mac"), 32)
	stretchedKey := append(encKey, macKey...)

	// Decrypt the protected symmetric key (format: "4.{b64iv}|{b64ct}|{b64mac}" or type 2).
	symKeyBytes, err := vwDecrypt(lr.Key, stretchedKey[:32], stretchedKey[32:])
	if err != nil {
		return nil, fmt.Errorf("decrypt protected symmetric key: %w", err)
	}
	if len(symKeyBytes) != 64 {
		return nil, fmt.Errorf("unexpected symmetric key length %d (expected 64)", len(symKeyBytes))
	}
	return symKeyBytes, nil
}

// vwHKDFExpand performs a single HKDF expand step: HMAC-SHA256(prk, info || 0x01).
func vwHKDFExpand(prk, info []byte, length int) []byte {
	h := hmac.New(sha256.New, prk)
	h.Write(info)
	h.Write([]byte{0x01})
	out := h.Sum(nil)
	return out[:length]
}

// ── Cipher fetching ───────────────────────────────────────────────────────────

type vwCipher struct {
	Type     int    `json:"Type"`
	FolderID string `json:"FolderId"`
	Name     string `json:"Name"`
	Notes    string `json:"Notes"`
}

func (p *VaultwardenProvider) fetchCiphers(ctx context.Context, base, bearer string) ([]vwCipher, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/ciphers", nil)
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
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var listResp struct {
		Data []vwCipher `json:"Data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, err
	}
	return listResp.Data, nil
}

type vwFolder struct {
	ID   string `json:"Id"`
	Name string `json:"Name"`
}

func (p *VaultwardenProvider) resolveFolderID(ctx context.Context, base, bearer string, symKey []byte, name string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/folders", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	resp, err := p.httpClient().Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var listResp struct {
		Data []vwFolder `json:"Data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return "", err
	}

	for _, f := range listResp.Data {
		decName, err := vwDecrypt(f.Name, symKey[:32], symKey[32:])
		if err != nil {
			continue
		}
		if string(decName) == name {
			return f.ID, nil
		}
	}
	return "", fmt.Errorf("folder %q not found", name)
}

// ── Decryption ────────────────────────────────────────────────────────────────

// vwDecrypt decrypts a Bitwarden/Vaultwarden encrypted string.
// Supports type 2 (AesCbc256_HmacSha256_B64: "2.iv|ct|mac") and
// type 4 (Rsa2048_OaepSha1_B64, not supported here — returns error).
func vwDecrypt(enc string, aesKey, macKey []byte) ([]byte, error) {
	if enc == "" {
		return nil, nil
	}

	// Determine type from prefix digit.
	dotIdx := strings.IndexByte(enc, '.')
	if dotIdx < 1 {
		return nil, fmt.Errorf("invalid encrypted string (no type prefix)")
	}
	encType := enc[:dotIdx]
	enc = enc[dotIdx+1:]

	switch encType {
	case "2": // AesCbc256_HmacSha256_B64
		return bwDecrypt("2."+enc, aesKey, macKey) // reuse Bitwarden SM decryptor
	case "0": // AesCbc256_B64 (no MAC — older format, some fields)
		parts := strings.SplitN(enc, "|", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid AesCbc256 string")
		}
		iv, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, err
		}
		ct, err := base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, err
		}
		block, err := aes.NewCipher(aesKey)
		if err != nil {
			return nil, err
		}
		if len(ct)%aes.BlockSize != 0 {
			return nil, fmt.Errorf("ciphertext not block-aligned")
		}
		plain := make([]byte, len(ct))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ct)
		return pkcs7Unpad(plain)
	default:
		return nil, fmt.Errorf("unsupported encryption type %s", encType)
	}
}
