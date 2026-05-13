package crucible.hipaa.phi_encryption

# HIPAA § 164.312(a)(2)(iv) — Encryption of ePHI at rest.
# RDS, DynamoDB, S3, and EFS resources must have encryption enabled.

import rego.v1

default allow := true

_unencrypted_rds contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_db_instance"
    change.change.actions[_] != "delete"
    change.change.after.storage_encrypted == false
    addr := change.address
}

_unencrypted_dynamodb contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_dynamodb_table"
    change.change.actions[_] != "delete"
    change.change.after.server_side_encryption[_].enabled == false
    addr := change.address
}

_unencrypted_efs contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_efs_file_system"
    change.change.actions[_] != "delete"
    change.change.after.encrypted == false
    addr := change.address
}

deny contains msg if {
    some addr in _unencrypted_rds
    msg := sprintf("HIPAA 164.312: RDS instance storage_encrypted=false: %s", [addr])
}

deny contains msg if {
    some addr in _unencrypted_dynamodb
    msg := sprintf("HIPAA 164.312: DynamoDB server-side encryption disabled: %s", [addr])
}

deny contains msg if {
    some addr in _unencrypted_efs
    msg := sprintf("HIPAA 164.312: EFS file system encrypted=false: %s", [addr])
}
