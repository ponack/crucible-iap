// SPDX-License-Identifier: AGPL-3.0-or-later
// Package chatops issues and validates short-lived signed action tokens that
// allow users to confirm, approve, or discard a run by clicking a URL embedded
// in a Slack / Teams / Discord / email notification — no extra app installation
// required. Tokens are HMAC-SHA256 over "runID:action:expiry_unix" and expire
// after 24 hours.
package chatops

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const tokenTTL = 24 * time.Hour

// GenerateToken returns a URL-safe token for the given run ID and action.
// The token encodes its own expiry so the server can reject stale links without
// a DB round-trip.
func GenerateToken(runID, action string, secretKey []byte) string {
	expiry := time.Now().Add(tokenTTL).Unix()
	mac := sign(runID, action, expiry, secretKey)
	return fmt.Sprintf("%d.%s", expiry, mac)
}

// ValidateToken returns true if the token is valid, unexpired, and matches
// the expected runID and action.
func ValidateToken(token, runID, action string, secretKey []byte) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	expiry, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return false
	}
	if time.Now().Unix() > expiry {
		return false
	}
	expected := sign(runID, action, expiry, secretKey)
	return hmac.Equal([]byte(parts[1]), []byte(expected))
}

func sign(runID, action string, expiry int64, secretKey []byte) string {
	mac := hmac.New(sha256.New, secretKey)
	fmt.Fprintf(mac, "%s:%s:%d", runID, action, expiry)
	return hex.EncodeToString(mac.Sum(nil))
}
