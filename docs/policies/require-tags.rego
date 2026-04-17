# require-tags.rego
# Type: post_plan
#
# Blocks any resource creation or update that is missing one or more required tags.
# Resources being deleted are exempt (they're going away anyway).
# Adjust REQUIRED_TAGS and EXEMPT_TYPES for your tagging strategy.

package crucible

# --- configuration ---
REQUIRED_TAGS := {"owner", "environment", "cost-centre"}

# Resource types that are not taggable (e.g. data sources, random providers).
EXEMPT_TYPES := {
  "random_pet",
  "random_integer",
  "random_string",
  "random_password",
  "null_resource",
  "terraform_data",
}
# ---------------------

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs contains msg if {
  some r in input.resource_changes
  not "delete" in r.change.actions
  not "no-op" in r.change.actions
  not EXEMPT_TYPES[r.type]
  tags := object.get(r.change.after, "tags", {})
  missing := REQUIRED_TAGS - {k | tags[k]}
  count(missing) > 0
  msg := sprintf(
    "resource %v is missing required tags: %v",
    [r.address, missing],
  )
}
