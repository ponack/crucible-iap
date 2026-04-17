# warn-on-delete.rego
# Type: post_plan
#
# Surfaces a non-blocking warning for every resource that would be deleted.
# The run can still be confirmed — the operator just sees the warning.
# Use this instead of no-destroy.rego when you want visibility without enforcement.

package crucible

plan := result if {
  result := {
    "deny":    [],
    "warn":    warn_msgs,
    "trigger": [],
  }
}

warn_msgs contains msg if {
  some r in input.resource_changes
  "delete" in r.change.actions
  msg := sprintf("⚠ resource %v will be deleted — confirm this is intentional", [r.address])
}
