package crucible.plan

# Example post-plan policy: block destroys of more than 5 resources.
# Input: { "run": {...}, "stack": {...}, "changes": { "add": N, "change": N, "destroy": N } }

default deny = []
default warn = []

deny[msg] {
	input.changes.destroy > 5
	msg := sprintf("destroying %d resources requires manual approval", [input.changes.destroy])
}

warn[msg] {
	input.changes.destroy > 0
	input.changes.destroy <= 5
	msg := sprintf("%d resources will be destroyed", [input.changes.destroy])
}

deny[msg] {
	input.run.type == "tracked"
	input.stack.auto_apply == true
	input.changes.destroy > 0
	msg := "auto-apply is blocked when resources are being destroyed"
}
