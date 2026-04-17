# approval-for-destroy.rego
# Type: approval
#
# Requires a human approval before any plan that would delete resources is applied.
# Works alongside warn-on-delete.rego (post_plan) — the warn fires first so the
# approver knows exactly which resources would be removed.

package crucible

import future.keywords.if

approval := result if {
  result := {
    "require_approval": require_approval,
    "reason":           reason,
  }
}

deletes := [r |
  some r in input.resource_changes
  "delete" in r.change.actions
]

require_approval := count(deletes) > 0

reason := msg if {
  require_approval
  msg := sprintf(
    "%v resource(s) will be deleted — approval required before apply",
    [count(deletes)],
  )
} else := ""
