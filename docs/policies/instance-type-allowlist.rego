# instance-type-allowlist.rego
# Type: post_plan
#
# Restricts AWS EC2 instance types to an approved list.
# Extend ALLOWED_TYPES with any instance families your org has approved.
# A separate warn rule flags instance types in a "caution" tier (e.g. m5, r5)
# that are permitted but should be reviewed.

package crucible

# --- configuration ---
ALLOWED_TYPES := {
  # General purpose — cost-effective
  "t3.micro", "t3.small", "t3.medium", "t3.large",
  "t3.xlarge", "t3.2xlarge",
  "t3a.micro", "t3a.small", "t3a.medium", "t3a.large",
  # Compute optimised
  "c5.large", "c5.xlarge", "c5.2xlarge",
  # Memory optimised — caution tier (warn)
  "r5.large", "r5.xlarge",
  # General purpose medium-large
  "m5.large", "m5.xlarge",
}

# Warn on these even though they're allowed — flag for review.
CAUTION_TYPES := {"r5.large", "r5.xlarge", "m5.large", "m5.xlarge"}
# ---------------------

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    warn_msgs,
    "trigger": [],
  }
}

deny_msgs contains msg if {
  some r in input.resource_changes
  r.type == "aws_instance"
  not "delete" in r.change.actions
  instance_type := r.change.after.instance_type
  not ALLOWED_TYPES[instance_type]
  msg := sprintf(
    "instance type %v is not in the approved list; choose from: %v",
    [instance_type, ALLOWED_TYPES],
  )
}

warn_msgs contains msg if {
  some r in input.resource_changes
  r.type == "aws_instance"
  not "delete" in r.change.actions
  instance_type := r.change.after.instance_type
  CAUTION_TYPES[instance_type]
  msg := sprintf(
    "instance type %v is in the caution tier — ensure this size is justified",
    [instance_type],
  )
}
