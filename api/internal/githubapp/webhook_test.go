// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func sign(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyGitHubSignature_Valid(t *testing.T) {
	secret := []byte("hookhookhookhook")
	body := []byte(`{"hello":"world"}`)
	if err := verifyGitHubSignature(sign(secret, body), body, secret); err != nil {
		t.Errorf("expected valid signature to pass, got %v", err)
	}
}

func TestVerifyGitHubSignature_WrongSecret(t *testing.T) {
	body := []byte(`payload`)
	good := sign([]byte("right"), body)
	if err := verifyGitHubSignature(good, body, []byte("wrong")); err == nil {
		t.Error("expected mismatch with wrong secret, got nil")
	}
}

func TestVerifyGitHubSignature_TamperedBody(t *testing.T) {
	secret := []byte("s3cret")
	good := sign(secret, []byte("original"))
	if err := verifyGitHubSignature(good, []byte("tampered"), secret); err == nil {
		t.Error("expected mismatch on tampered body, got nil")
	}
}

func TestVerifyGitHubSignature_MalformedHeader(t *testing.T) {
	cases := []string{"", "sha1=abcd", "sha256=zzzz"}
	for _, c := range cases {
		if err := verifyGitHubSignature(c, []byte("body"), []byte("secret")); err == nil {
			t.Errorf("expected error for header %q", c)
		}
	}
}
