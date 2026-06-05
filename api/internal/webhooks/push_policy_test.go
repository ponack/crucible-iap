// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import "testing"

func TestPushPolicySkipReason(t *testing.T) {
	tests := []struct {
		name          string
		skipMessages  []string
		skipActors    []string
		commitMessage string
		actor         string
		want          string
	}{
		{
			name: "no filters configured → allow",
			want: "",
		},
		{
			name:          "no matching filter → allow",
			skipMessages:  []string{"[skip ci]"},
			skipActors:    []string{"dependabot[bot]"},
			commitMessage: "fix: real change",
			actor:         "alice",
			want:          "",
		},
		{
			name:          "commit-message substring match",
			skipMessages:  []string{"[skip ci]"},
			commitMessage: "chore: bump deps [skip ci]",
			want:          "skip_commit_message:[skip ci]",
		},
		{
			name:          "commit-message match is case-sensitive (per spec)",
			skipMessages:  []string{"[skip ci]"},
			commitMessage: "chore: bump deps [SKIP CI]",
			want:          "",
		},
		{
			name:         "actor match is case-insensitive",
			skipActors:   []string{"Dependabot[bot]"},
			actor:        "dependabot[bot]",
			want:         "skip_actor:Dependabot[bot]",
		},
		{
			name:         "actor match strips whitespace on configured entry",
			skipActors:   []string{"  renovate[bot]  "},
			actor:        "renovate[bot]",
			want:         "skip_actor:  renovate[bot]  ",
		},
		{
			name:          "empty string entries are skipped (no accidental match-all)",
			skipMessages:  []string{""},
			skipActors:    []string{""},
			commitMessage: "any commit",
			actor:         "anyone",
			want:          "",
		},
		{
			name:          "commit-message check happens before actor check",
			skipMessages:  []string{"[skip ci]"},
			skipActors:    []string{"dependabot[bot]"},
			commitMessage: "[skip ci] dep bump",
			actor:         "dependabot[bot]",
			want:          "skip_commit_message:[skip ci]",
		},
		{
			name:         "empty commit message does not match anything",
			skipMessages: []string{"anything"},
			actor:        "alice",
			want:         "",
		},
		{
			name:       "empty actor does not match anything",
			skipActors: []string{"alice"},
			want:       "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pushPolicySkipReason(tc.skipMessages, tc.skipActors, tc.commitMessage, tc.actor)
			if got != tc.want {
				t.Errorf("pushPolicySkipReason(skipMsgs=%v, skipActors=%v, msg=%q, actor=%q) = %q, want %q",
					tc.skipMessages, tc.skipActors, tc.commitMessage, tc.actor, got, tc.want)
			}
		})
	}
}
