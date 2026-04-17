# restrict-regions.rego
# Type: post_plan
#
# Blocks resources deployed to AWS regions outside of an approved set.
# Reads the region from the provider configuration embedded in the plan.
# Adjust ALLOWED_REGIONS to match your data-residency or compliance requirements.

package crucible

# --- configuration ---
ALLOWED_REGIONS := {"eu-west-1", "eu-west-2", "eu-central-1"}
# ---------------------

plan := result if {
  result := {
    "deny":    deny_msgs,
    "warn":    [],
    "trigger": [],
  }
}

deny_msgs contains msg if {
  some addr, cfg in input.configuration.provider_config
  cfg.name == "aws"
  region := cfg.expressions.region.constant_value
  not ALLOWED_REGIONS[region]
  msg := sprintf(
    "AWS provider %v is configured for region %v which is not in the approved set %v",
    [addr, region, ALLOWED_REGIONS],
  )
}
