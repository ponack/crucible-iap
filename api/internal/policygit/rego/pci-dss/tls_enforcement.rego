package crucible.pcidss.tls_enforcement

# PCI-DSS Requirement 4.2 — Never send unprotected PANs over open public networks.
# ELB/ALB listeners must not use HTTP without a redirect to HTTPS.
# RDS must require SSL connections.

import rego.v1

default allow := true

_http_listener contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_lb_listener"
    change.change.actions[_] != "delete"
    change.change.after.protocol == "HTTP"
    change.change.after.default_action[_].type != "redirect"
    addr := change.address
}

_rds_no_ssl contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_db_instance"
    change.change.actions[_] != "delete"
    not change.change.after.ca_cert_identifier
    addr := change.address
}

warn contains msg if {
    some addr in _http_listener
    msg := sprintf("PCI-DSS Req 4.2: ALB listener uses HTTP without redirect: %s", [addr])
}

warn contains msg if {
    some addr in _rds_no_ssl
    msg := sprintf("PCI-DSS Req 4.2: RDS instance has no CA certificate configured: %s", [addr])
}
