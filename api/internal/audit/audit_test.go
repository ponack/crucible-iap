// SPDX-License-Identifier: AGPL-3.0-or-later
package audit

import "testing"

func TestNilIfEmpty(t *testing.T) {
	if nilIfEmpty("") != nil {
		t.Error("empty string should return nil")
	}
	v := nilIfEmpty("hello")
	s, ok := v.(string)
	if !ok || s != "hello" {
		t.Errorf("expected string 'hello', got %v", v)
	}
}

func TestParseIP(t *testing.T) {
	tests := []struct {
		input   string
		wantNil bool
		want    string
	}{
		{"", true, ""},
		{"not-an-ip", true, ""},
		{"192.168.1.1", false, "192.168.1.1"},
		{"::1", false, "::1"},
		{"2001:db8::1", false, "2001:db8::1"},
	}
	for _, tt := range tests {
		v := parseIP(tt.input)
		if tt.wantNil {
			if v != nil {
				t.Errorf("parseIP(%q) = %v, want nil", tt.input, v)
			}
			continue
		}
		s, ok := v.(string)
		if !ok {
			t.Errorf("parseIP(%q) returned %T, want string", tt.input, v)
			continue
		}
		if s != tt.want {
			t.Errorf("parseIP(%q) = %q, want %q", tt.input, s, tt.want)
		}
	}
}
