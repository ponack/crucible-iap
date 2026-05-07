// SPDX-License-Identifier: AGPL-3.0-or-later
package githubapp

import (
	"strings"
	"testing"
)

const testSecret = "test-secret-key-must-be-at-least-32-chars"

func TestInstallState_RoundTrip(t *testing.T) {
	state, err := SignInstallState(testSecret, "app-uuid-1")
	if err != nil {
		t.Fatalf("SignInstallState: %v", err)
	}
	got, err := VerifyInstallState(testSecret, state)
	if err != nil {
		t.Fatalf("VerifyInstallState: %v", err)
	}
	if got != "app-uuid-1" {
		t.Errorf("got app uuid %q, want app-uuid-1", got)
	}
}

func TestInstallState_BadSignature(t *testing.T) {
	state, _ := SignInstallState(testSecret, "app-uuid-1")
	if _, err := VerifyInstallState("different-secret-thirtytwo-chars", state); err == nil {
		t.Error("expected error on different secret, got nil")
	}
}

func TestInstallState_Tampered(t *testing.T) {
	state, _ := SignInstallState(testSecret, "app-uuid-1")
	parts := strings.SplitN(state, ".", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed state %q", state)
	}
	tampered := parts[0] + "X." + parts[1]
	if _, err := VerifyInstallState(testSecret, tampered); err == nil {
		t.Error("expected error on tampered payload, got nil")
	}
}

func TestInstallState_Malformed(t *testing.T) {
	if _, err := VerifyInstallState(testSecret, "no-dot"); err == nil {
		t.Error("expected error on malformed state")
	}
}
