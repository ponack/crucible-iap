package crucible.soc2.logging_required

# SOC 2 CC7.2 — System monitoring and logging.
# CloudTrail must be enabled and S3 buckets should have access logging.

import rego.v1

default allow := true

_cloudtrail_disabled contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_cloudtrail"
    change.change.actions[_] != "delete"
    change.change.after.enable_logging == false
    addr := change.address
}

deny contains msg if {
    some addr in _cloudtrail_disabled
    msg := sprintf("SOC 2 CC7.2: CloudTrail has logging disabled: %s", [addr])
}
