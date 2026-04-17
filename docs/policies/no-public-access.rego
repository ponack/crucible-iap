# no-public-access.rego
# Type: post_plan
#
# Blocks S3 buckets and security groups that expose resources to the public internet.
# Checks:
#   - aws_s3_bucket_public_access_block: all four block_public_* flags must be true
#   - aws_security_group / aws_vpc_security_group_ingress_rule: no ingress from 0.0.0.0/0 or ::/0

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
  r.type == "aws_s3_bucket_public_access_block"
  not "delete" in r.change.actions
  cfg := r.change.after
  some flag in ["block_public_acls", "block_public_policy", "ignore_public_acls", "restrict_public_buckets"]
  not cfg[flag] == true
  msg := sprintf(
    "resource %v: %v must be true to prevent public S3 access",
    [r.address, flag],
  )
}

deny_msgs contains msg if {
  some r in input.resource_changes
  r.type == "aws_security_group"
  not "delete" in r.change.actions
  some rule in r.change.after.ingress
  _is_public_cidr(rule)
  msg := sprintf(
    "resource %v has an ingress rule open to the public internet (%v) — restrict to known CIDRs",
    [r.address, _public_cidr(rule)],
  )
}

deny_msgs contains msg if {
  some r in input.resource_changes
  r.type == "aws_vpc_security_group_ingress_rule"
  not "delete" in r.change.actions
  _is_public_cidr(r.change.after)
  msg := sprintf(
    "resource %v is open to the public internet (%v) — restrict to known CIDRs",
    [r.address, _public_cidr(r.change.after)],
  )
}

_public_cidrs := {"0.0.0.0/0", "::/0"}

_is_public_cidr(rule) if {
  _public_cidrs[rule.cidr_blocks[_]]
}

_is_public_cidr(rule) if {
  _public_cidrs[rule.ipv6_cidr_blocks[_]]
}

_is_public_cidr(rule) if {
  _public_cidrs[rule.cidr_ipv4]
}

_is_public_cidr(rule) if {
  _public_cidrs[rule.cidr_ipv6]
}

_public_cidr(rule) := cidr if {
  some cidr in _public_cidrs
  rule.cidr_blocks[_] == cidr
} else := cidr if {
  some cidr in _public_cidrs
  rule.ipv6_cidr_blocks[_] == cidr
} else := rule.cidr_ipv4 if {
  _public_cidrs[rule.cidr_ipv4]
} else := rule.cidr_ipv6 if {
  _public_cidrs[rule.cidr_ipv6]
}
