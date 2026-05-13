package crucible.cisaws.s3_block_public_access

# CIS AWS 2.1.5 — Ensure S3 buckets have Block Public Access settings enabled.

import rego.v1

default allow := true

_public_bucket contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_s3_bucket_public_access_block"
    change.change.actions[_] != "delete"
    not change.change.after.block_public_acls
    addr := change.address
}

_public_bucket contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_s3_bucket_public_access_block"
    change.change.actions[_] != "delete"
    not change.change.after.block_public_policy
    addr := change.address
}

warn contains msg if {
    some addr in _public_bucket
    msg := sprintf("CIS AWS 2.1.5: S3 bucket public access not fully blocked: %s", [addr])
}
