package crucible.pcidss.no_public_ingress

# PCI-DSS Requirement 1.3 — Prohibit direct public access between the internet
# and any component in the cardholder data environment.
# Security groups must not allow unrestricted inbound access on sensitive ports.

import rego.v1

default allow := true

_sensitive_ports := {22, 3306, 5432, 1433, 6379, 27017, 9200}

_open_ingress contains addr if {
    some change in input.plan.resource_changes
    change.type == "aws_security_group"
    change.change.actions[_] != "delete"
    some rule in change.change.after.ingress
    rule.cidr_blocks[_] == "0.0.0.0/0"
    rule.from_port <= port
    rule.to_port >= port
    port := _sensitive_ports[_]
    addr := change.address
}

deny contains msg if {
    some addr in _open_ingress
    msg := sprintf("PCI-DSS Req 1.3: security group allows unrestricted inbound on sensitive port: %s", [addr])
}
