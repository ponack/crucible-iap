package crucible.cisaws.iam_password_policy

# CIS AWS 1.8–1.11 — IAM password policy requirements.
# Minimum length 14, require uppercase, lowercase, numbers, symbols.

import rego.v1

default allow := true

_weak_policy contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_iam_account_password_policy"
    change.change.actions[_] != "delete"
    change.change.after.minimum_password_length < 14
    addr := change.address
}

deny contains msg if {
    some addr in _weak_policy
    msg := sprintf("CIS AWS 1.8: IAM password policy minimum_password_length must be >= 14: %s", [addr])
}
