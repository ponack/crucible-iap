package crucible.soc2.approval_required

# SOC 2 CC6.1 — Logical and physical access controls.
# All infrastructure changes must go through an approval workflow before apply.

import rego.v1

default allow := false

allow if {
    input.run.type == "proposed"
}

deny contains msg if {
    input.run.type == "tracked"
    not input.run.approved
    msg := "SOC 2 CC6.1: tracked runs require approval before apply"
}
