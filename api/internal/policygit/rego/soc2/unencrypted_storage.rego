package crucible.soc2.unencrypted_storage

# SOC 2 CC6.7 — Encryption of data at rest.
# S3 buckets and RDS instances must have encryption enabled.

import rego.v1

default allow := true

_unencrypted_s3 contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_s3_bucket_server_side_encryption_configuration"
    change.change.actions[_] != "delete"
    not change.change.after.rule[_].apply_server_side_encryption_by_default[_].sse_algorithm
    addr := change.address
}

_unencrypted_rds contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_db_instance"
    change.change.actions[_] != "delete"
    change.change.after.storage_encrypted == false
    addr := change.address
}

deny contains msg if {
    some addr in _unencrypted_s3
    msg := sprintf("SOC 2 CC6.7: S3 bucket missing encryption configuration: %s", [addr])
}

deny contains msg if {
    some addr in _unencrypted_rds
    msg := sprintf("SOC 2 CC6.7: RDS instance storage_encrypted=false: %s", [addr])
}
