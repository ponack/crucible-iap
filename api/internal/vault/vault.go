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

	"golang.org/x/crypto/hkdf"
)

// Vault encrypts and decrypts secret values using a master key.
type Vault struct {
	masterKey []byte
}

// New creates a Vault from the application secret key string.
func New(secretKey string) *Vault {
	// SHA-256 the key so any length becomes a valid 32-byte AES key.
	h := sha256.Sum256([]byte(secretKey))
	return &Vault{masterKey: h[:]}
}

// Encrypt encrypts plaintext for the given stackID using AES-256-GCM.
// The nonce (12 bytes) is prepended to the ciphertext in the returned slice.
func (v *Vault) Encrypt(stackID string, plaintext []byte) ([]byte, error) {
	key, err := v.deriveKey(stackID)
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

	ciphertext := gcm.Seal(nil, nonce, plaintext, nil)
	return append(nonce, ciphertext...), nil
}

// Decrypt decrypts a value produced by Encrypt.
func (v *Vault) Decrypt(stackID string, data []byte) ([]byte, error) {
	key, err := v.deriveKey(stackID)
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

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}

	return plaintext, nil
}

// deriveKey produces a 32-byte AES key scoped to a specific stack using
// HKDF-SHA256. Using the stack ID as "info" ensures each stack gets a
// unique key even if the master key is shared.
func (v *Vault) deriveKey(stackID string) ([]byte, error) {
	r := hkdf.New(sha256.New, v.masterKey, nil, []byte("crucible-stack-envvar:"+stackID))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, err
	}
	return key, nil
}
