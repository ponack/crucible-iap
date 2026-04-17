# blast-radius-guard.rego
# Type: post_plan
#
# Blocks plans that change more than MAX_CHANGES resources at once.
# Warns when the count exceeds WARN_THRESHOLD (but is still within the hard limit).
# Adjust the thresholds to suit your environment.

package crucible

# --- configuration ---
MAX_CHANGES    := 20
WARN_THRESHOLD := 10
# ---------------------

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

changing := [r |
  some r in input.resource_changes
  not "no-op" in r.change.actions
]

deny_msgs contains msg if {
  count(changing) > MAX_CHANGES
  msg := sprintf(
    "plan modifies %v resources — hard limit is %v; split into smaller incremental changes",
    [count(changing), MAX_CHANGES],
  )
}

warn_msgs contains msg if {
  count(changing) > WARN_THRESHOLD
  count(changing) <= MAX_CHANGES
  msg := sprintf(
    "plan modifies %v resources (warning threshold is %v)",
    [count(changing), WARN_THRESHOLD],
  )
}
