// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import "testing"

func TestPathsMatchAnyGlob(t *testing.T) {
	tests := []struct {
		name  string
		globs []string
		paths []string
		want  bool
	}{
		{
			name:  "no globs configured allows everything",
			globs: nil,
			paths: []string{"any/path.tf"},
			want:  true,
		},
		{
			name:  "empty paths falls through to true (forge omitted changed-file data)",
			globs: []string{"apps/**"},
			paths: nil,
			want:  true,
		},
		{
			name:  "exact path match",
			globs: []string{"infra/main.tf"},
			paths: []string{"infra/main.tf"},
			want:  true,
		},
		{
			name:  "exact path miss",
			globs: []string{"infra/main.tf"},
			paths: []string{"infra/other.tf"},
			want:  false,
		},
		{
			name:  "double-star descends directories",
			globs: []string{"apps/checkout/**"},
			paths: []string{"apps/checkout/iam/role.tf"},
			want:  true,
		},
		{
			name:  "single-star stays in one segment",
			globs: []string{"apps/*.tf"},
			paths: []string{"apps/checkout/iam/role.tf"},
			want:  false,
		},
		{
			name:  "extension glob",
			globs: []string{"**/*.tf"},
			paths: []string{"deep/nested/path/main.tf"},
			want:  true,
		},
		{
			name:  "any-of: one matching path is enough",
			globs: []string{"apps/checkout/**"},
			paths: []string{"unrelated/file.md", "apps/checkout/iam/role.tf"},
			want:  true,
		},
		{
			name:  "any-of: one matching glob is enough",
			globs: []string{"apps/billing/**", "apps/checkout/**"},
			paths: []string{"apps/checkout/main.tf"},
			want:  true,
		},
		{
			name:  "all miss",
			globs: []string{"apps/billing/**"},
			paths: []string{"apps/checkout/main.tf", "docs/readme.md"},
			want:  false,
		},
		{
			name:  "empty-string entries are skipped (typo safety)",
			globs: []string{"", "apps/checkout/**", ""},
			paths: []string{"apps/checkout/main.tf"},
			want:  true,
		},
		{
			name:  "invalid glob is silently skipped, others still evaluated",
			globs: []string{"[unclosed", "apps/checkout/**"},
			paths: []string{"apps/checkout/main.tf"},
			want:  true,
		},
		{
			name:  "all globs invalid → treated as no filter, returns true",
			globs: []string{"[unclosed", "[also-bad"},
			paths: []string{"some/path.tf"},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pathsMatchAnyGlob(tc.globs, tc.paths)
			if got != tc.want {
				t.Errorf("pathsMatchAnyGlob(%v, %v) = %v, want %v", tc.globs, tc.paths, got, tc.want)
			}
		})
	}
}
