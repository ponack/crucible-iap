# approval-high-blast-radius.rego
# Type: approval
#
# Requires human approval when a plan changes more than APPROVAL_THRESHOLD resources.
# Pair with blast-radius-guard.rego (post_plan) if you also want a hard upper limit.
# The threshold here is intentionally lower — warn early, gate later.

package crucible

import future.keywords.if

# --- configuration ---
APPROVAL_THRESHOLD := 5
# ---------------------

approval := result if {
  result := {
    "require_approval": require_approval,
    "reason":           reason,
  }
}

changing := [r |
  some r in input.resource_changes
  not "no-op" in r.change.actions
]

require_approval := count(changing) > APPROVAL_THRESHOLD

reason := msg if {
  require_approval
  msg := sprintf(
    "plan modifies %v resources (threshold: %v) — approval required before apply",
    [count(changing), APPROVAL_THRESHOLD],
  )
} else := ""
