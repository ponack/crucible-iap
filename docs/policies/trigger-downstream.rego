# trigger-downstream.rego
# Type: post_apply
#
# After a successful apply, triggers a downstream stack run.
# Useful for fan-out pipelines: e.g. a shared-infra stack finishing should
# kick off per-environment stacks that depend on its outputs.
#
# Set TRIGGER_STACK_IDS to the Crucible stack IDs you want to trigger.
# The triggered stacks will receive a plan run (not an auto-apply) by default.

package crucible

import future.keywords.if

# --- configuration ---
TRIGGER_STACK_IDS := [
  # "stk_abc123",
  # "stk_def456",
]
# ---------------------

plan := result if {
  result := {
    "deny":    [],
    "warn":    warn_msgs,
    "trigger": triggers,
  }
}

triggers := [t |
  some id in TRIGGER_STACK_IDS
  t := {"stack_id": id}
]

warn_msgs := [msg |
  count(TRIGGER_STACK_IDS) > 0
  some id in TRIGGER_STACK_IDS
  msg := sprintf("triggering downstream stack %v", [id])
]
