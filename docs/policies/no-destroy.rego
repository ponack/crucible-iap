# no-destroy.rego
# Type: post_plan
#
# Hard-blocks any run that would delete one or more resources.
# Recommended as an org default on production stacks.
# Use an explicit destroy run type for intentional teardowns.

package crucible

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs contains msg if {
  some r in input.resource_changes
  "delete" in r.change.actions
  msg := sprintf(
    "resource %v would be destroyed — destroy operations must be triggered as an explicit destroy run",
    [r.address],
  )
}
