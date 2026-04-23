// SPDX-License-Identifier: AGPL-3.0-or-later
// Package tokenauth provides argon2id hashing and verification for API tokens.
package tokenauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	// argon2id parameters — OWASP minimum for interactive auth.
	// At these settings verification takes ~50 ms on a single core.
	memory      uint32 = 32 * 1024
	iterations  uint32 = 2
	parallelism uint8  = 1
	keyLen      uint32 = 32
	saltLen            = 16

	VersionArgon2id = "argon2id"
	VersionSHA256   = "sha256"
)

// Hash computes an argon2id hash of secret.
// The returned string has the form "<salthex>:<keyhex>" and is safe to store
// in token_hash alongside hash_version = "argon2id".
func Hash(secret string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(secret), salt, iterations, memory, parallelism, keyLen)
	return hex.EncodeToString(salt) + ":" + hex.EncodeToString(key), nil
}

// Verify checks secret against stored and version.
// For argon2id: stored must be "<salthex>:<keyhex>".
// For sha256:   stored must be the hex-encoded SHA-256 digest of the secret.
// All comparisons are constant-time.
func Verify(secret, stored, version string) (bool, error) {
	switch version {
	case VersionArgon2id:
		parts := strings.SplitN(stored, ":", 2)
		if len(parts) != 2 {
			return false, fmt.Errorf("malformed argon2id hash")
		}
		salt, err := hex.DecodeString(parts[0])
		if err != nil {
			return false, err
		}
		storedKey, err := hex.DecodeString(parts[1])
		if err != nil {
			return false, err
		}
		computed := argon2.IDKey([]byte(secret), salt, iterations, memory, parallelism, uint32(len(storedKey)))
		return subtle.ConstantTimeCompare(computed, storedKey) == 1, nil
	default: // "sha256"
		h := sha256.Sum256([]byte(secret))
		computedHex := hex.EncodeToString(h[:])
		return subtle.ConstantTimeCompare([]byte(computedHex), []byte(stored)) == 1, nil
	}
}
