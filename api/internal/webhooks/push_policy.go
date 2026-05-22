// SPDX-License-Identifier: AGPL-3.0-or-later
package webhooks

import "strings"

// pushPolicySkipReason returns a non-empty reason string if the event matches
// a configured skip filter, or "" if the event should be allowed through.
// Returning the reason lets the caller log it in the webhook delivery record.
//
//   - commitMessage is matched as a case-sensitive substring against each
//     entry in skipMessages.
//   - actor is matched as a case-insensitive equality against each entry in
//     skipActors. Designed for bot logins like "dependabot[bot]".
func pushPolicySkipReason(skipMessages, skipActors []string, commitMessage, actor string) string {
	if commitMessage != "" {
		for _, pat := range skipMessages {
			if pat == "" {
				continue
			}
			if strings.Contains(commitMessage, pat) {
				return "skip_commit_message:" + pat
			}
		}
	}
	if actor != "" {
		al := strings.ToLower(actor)
		for _, a := range skipActors {
			if a == "" {
				continue
			}
			if strings.ToLower(strings.TrimSpace(a)) == al {
				return "skip_actor:" + a
			}
		}
	}
	return ""
}
