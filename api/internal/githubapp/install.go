// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// statePayload is what we round-trip through GitHub's install flow as the
// `state` query param so the callback can identify which app the user was
// installing and prove the request originated from us.
type statePayload struct {
	AppUUID string `json:"a"`
	Nonce   string `json:"n"`
	Exp     int64  `json:"e"`
}

// SignInstallState produces a tamper-proof state value bound to a specific app
// and the current Crucible secret key. Valid for 10 minutes.
func SignInstallState(secretKey, appUUID string) (string, error) {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	payload := statePayload{
		AppUUID: appUUID,
		Nonce:   base64.RawURLEncoding.EncodeToString(nonce),
		Exp:     time.Now().Add(10 * time.Minute).Unix(),
	}
	body, _ := json.Marshal(payload)
	encBody := base64.RawURLEncoding.EncodeToString(body)
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(encBody))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return encBody + "." + sig, nil
}

// VerifyInstallState validates a state string produced by SignInstallState and
// returns the embedded appUUID. Errors on bad signature or expired payload.
func VerifyInstallState(secretKey, state string) (string, error) {
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		return "", errors.New("malformed state")
	}
	mac := hmac.New(sha256.New, []byte(secretKey))
	mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return "", errors.New("invalid state signature")
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("decode state: %w", err)
	}
	var payload statePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("parse state: %w", err)
	}
	if time.Now().Unix() > payload.Exp {
		return "", errors.New("state expired")
	}
	if payload.AppUUID == "" {
		return "", errors.New("state missing app id")
	}
	return payload.AppUUID, nil
}
