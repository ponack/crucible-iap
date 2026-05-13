package crucible.cisaws.iam_root_no_access_keys

# CIS AWS 1.4 — Ensure no root account access key exists.
# Creating IAM access keys for the root account is prohibited.

import rego.v1

default allow := true

_root_keys contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_iam_access_key"
    change.change.actions[_] != "delete"
    change.change.after.user == "root"
    addr := change.address
}

deny contains msg if {
    some addr in _root_keys
    msg := sprintf("CIS AWS 1.4: root account access key must not be created: %s", [addr])
}
