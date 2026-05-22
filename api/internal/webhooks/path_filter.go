// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import "github.com/gobwas/glob"

// pathsMatchAnyGlob reports whether at least one path matches at least one
// glob pattern. Used by the monorepo path-filter feature to gate webhook-
// driven runs.
//
// Globs use the gobwas/glob syntax — `**` matches any number of path
// segments, `*` matches within a single segment, `?` matches one char,
// `[abc]` is a character class. Examples:
//
//	apps/checkout/**       any file under apps/checkout/
//	**/*.tf                any .tf file anywhere in the repo
//	infra/main.tf          exact path
//
// Behaviour:
//   - Empty `globs` → always returns true (no filter configured).
//   - Empty `paths` → returns true (best-effort: forges that don't include
//     changed files in the payload — Bitbucket / ADO push, all PR events —
//     fall through to the existing branch / lifecycle gating).
//   - Invalid glob patterns are silently skipped so a typo can't lock a
//     stack out of all runs.
func pathsMatchAnyGlob(globs, paths []string) bool {
	if len(globs) == 0 || len(paths) == 0 {
		return true
	}
	compiled := make([]glob.Glob, 0, len(globs))
	for _, g := range globs {
		if g == "" {
			continue
		}
		c, err := glob.Compile(g, '/')
		if err != nil {
			continue
		}
		compiled = append(compiled, c)
	}
	if len(compiled) == 0 {
		return true
	}
	for _, p := range paths {
		for _, c := range compiled {
			if c.Match(p) {
				return true
			}
		}
	}
	return false
}
