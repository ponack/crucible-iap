package crucible.hipaa.audit_logging

# HIPAA § 164.312(b) — Audit controls: hardware, software, and procedural
# mechanisms that record and examine activity in information systems.
# CloudTrail must be enabled with log file validation.

import rego.v1

default allow := true

_no_validation contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_cloudtrail"
    change.change.actions[_] != "delete"
    change.change.after.enable_log_file_validation == false
    addr := change.address
}

deny contains msg if {
    some addr in _no_validation
    msg := sprintf("HIPAA 164.312(b): CloudTrail log file validation must be enabled: %s", [addr])
}
