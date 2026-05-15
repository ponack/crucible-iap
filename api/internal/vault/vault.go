// SPDX-License-Identifier: AGPL-3.0-or-later
// Package vault provides AES-256-GCM encryption for stack-level secrets.
// Keys are derived per-stack using HKDF-SHA256 from the master secret key,
// so compromising one stack's ciphertext does not expose other stacks.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/hkdf"
)

// Vault encrypts and decrypts secret values using a master key. The master
// can be swapped at runtime by the BYOK control plane (EnableKMS, RotateMasterKey,
// DisableKMS); the RWMutex serialises swaps against in-flight derivations so
// every encryption op sees a consistent master+salt pair.
type Vault struct {
	mu        sync.RWMutex
	masterKey []byte
	salt      []byte // nil = legacy nil-salt HKDF (pre-migration only)
}

// New creates a Vault from the application secret key string using nil HKDF
// salt. Only used during vault data migration to decrypt legacy ciphertext;
// production code should use LoadOrCreate which provides a deployment salt.
func New(secretKey string) *Vault {
	h := sha256.Sum256([]byte(secretKey))
	return &Vault{masterKey: h[:]}
}

// NewWithSalt creates a Vault with a deployment-unique HKDF salt. All new
// encryptions and post-migration decryptions use this form.
func NewWithSalt(secretKey string, salt []byte) *Vault {
	h := sha256.Sum256([]byte(secretKey))
	return &Vault{masterKey: h[:], salt: salt}
}

// NewWithMaster creates a Vault from a raw 32-byte master key. Used when the
// master is held in an external KMS — the wrapped blob is unwrapped at boot
// and the raw bytes passed in here. Salt and HKDF derivation are unchanged.
func NewWithMaster(masterKey, salt []byte) *Vault {
	return &Vault{masterKey: masterKey, salt: salt}
}

// deriveSecretKeyMaster returns the 32-byte master key derived from a secret
// string the same way New / NewWithSalt do. Exposed inside the package so the
// BYOK control plane can recover the legacy master on DisableKMS.
func deriveSecretKeyMaster(secretKey string) []byte {
	h := sha256.Sum256([]byte(secretKey))
	return h[:]
}

// Encrypt encrypts plaintext for the given stackID using AES-256-GCM.
// The nonce (12 bytes) is prepended to the ciphertext in the returned slice.
func (v *Vault) Encrypt(stackID string, plaintext []byte) ([]byte, error) {
	return v.encryptWithContext("crucible-stack-envvar:"+stackID, plaintext)
}

// Decrypt decrypts a value produced by Encrypt.
func (v *Vault) Decrypt(stackID string, data []byte) ([]byte, error) {
	return v.decryptWithContext("crucible-stack-envvar:"+stackID, data)
}

// EncryptFor encrypts plaintext scoped to an arbitrary context string.
// Use this for non-stack resources (e.g. org integrations) where the context
// key should be something other than a stack ID.
func (v *Vault) EncryptFor(context string, plaintext []byte) ([]byte, error) {
	return v.encryptWithContext(context, plaintext)
}

// DecryptFor decrypts a value produced by EncryptFor with the same context.
func (v *Vault) DecryptFor(context string, data []byte) ([]byte, error) {
	return v.decryptWithContext(context, data)
}

func (v *Vault) encryptWithContext(ctx string, plaintext []byte) ([]byte, error) {
	key, err := v.deriveKeyFor(ctx)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate nonce: %w", err)
	}
	return append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...), nil
}

func (v *Vault) decryptWithContext(ctx string, data []byte) ([]byte, error) {
	key, err := v.deriveKeyFor(ctx)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("new cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("new gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

func (v *Vault) deriveKeyFor(info string) ([]byte, error) {
	v.mu.RLock()
	master := v.masterKey
	salt := v.salt
	v.mu.RUnlock()
	r := hkdf.New(sha256.New, master, salt, []byte(info))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}

// swapMaster atomically replaces the master key. Used by the BYOK control
// plane after a successful re-encryption transition. Salt is unchanged.
func (v *Vault) swapMaster(newMaster []byte) {
	v.mu.Lock()
	v.masterKey = newMaster
	v.mu.Unlock()
}

// currentMaster returns a copy of the master key under a read lock — used by
// BYOK transitions to construct the throwaway "old vault" they decrypt with.
func (v *Vault) currentMaster() []byte {
	v.mu.RLock()
	defer v.mu.RUnlock()
	m := make([]byte, len(v.masterKey))
	copy(m, v.masterKey)
	return m
}
