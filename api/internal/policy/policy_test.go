// SPDX-License-Identifier: AGPL-3.0-or-later
package policy_test

import (
	"context"
	"testing"

	"github.com/ponack/crucible-iap/internal/policy"
)

const allowAllPolicy = `
package crucible

plan := {"deny": [], "warn": [], "trigger": []}
`

const denyDestroyPolicy = `
package crucible

plan := result if {
	result := {
		"deny":    deny_msgs,
		"warn":    warn_msgs,
		"trigger": [],
	}
}

deny_msgs contains msg if {
	input.resource_changes[_].change.actions[_] == "delete"
	msg := "destroy operations are not permitted by policy"
}

warn_msgs contains msg if {
	input.resource_changes[_].change.actions[_] == "update"
	msg := "resource update detected"
}
`

const loginPolicy = `
package crucible

login := result if {
	result := {
		"deny": deny_msgs,
		"warn": [],
	}
}

deny_msgs contains msg if {
	count(input.groups) == 0
	msg := "users must belong to at least one group"
}
`

func TestEngine_AllowAll(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	if err := e.Load(ctx, "p1", "allow-all", policy.TypePostPlan, allowAllPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := e.Evaluate(ctx, policy.TypePostPlan, map[string]any{
		"resource_changes": []any{},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Allow {
		t.Errorf("expected Allow=true, got false; denies: %v", result.Deny)
	}
}

func TestEngine_DenyDestroy(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	if err := e.Load(ctx, "p1", "deny-destroy", policy.TypePostPlan, denyDestroyPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	input := map[string]any{
		"resource_changes": []any{
			map[string]any{
				"address": "aws_instance.web",
				"change":  map[string]any{"actions": []any{"delete"}},
			},
		},
	}

	result, err := e.Evaluate(ctx, policy.TypePostPlan, input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Allow {
		t.Error("expected Allow=false for destroy, got true")
	}
	if len(result.Deny) == 0 {
		t.Error("expected at least one deny message")
	}
}

func TestEngine_WarnOnUpdate(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	if err := e.Load(ctx, "p1", "deny-destroy", policy.TypePostPlan, denyDestroyPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	input := map[string]any{
		"resource_changes": []any{
			map[string]any{
				"address": "aws_instance.web",
				"change":  map[string]any{"actions": []any{"update"}},
			},
		},
	}

	result, err := e.Evaluate(ctx, policy.TypePostPlan, input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Allow {
		t.Errorf("expected Allow=true for update-only plan, got false; denies: %v", result.Deny)
	}
	if len(result.Warn) == 0 {
		t.Error("expected at least one warning for update")
	}
}

func TestEngine_MultiplePoliciesMerged(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	// Load two policies — one allows all, one denies destroy.
	if err := e.Load(ctx, "p1", "allow-all", policy.TypePostPlan, allowAllPolicy); err != nil {
		t.Fatalf("Load p1: %v", err)
	}
	if err := e.Load(ctx, "p2", "deny-destroy", policy.TypePostPlan, denyDestroyPolicy); err != nil {
		t.Fatalf("Load p2: %v", err)
	}

	input := map[string]any{
		"resource_changes": []any{
			map[string]any{
				"change": map[string]any{"actions": []any{"delete"}},
			},
		},
	}

	result, err := e.Evaluate(ctx, policy.TypePostPlan, input)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	// Even with one allow-all policy, a deny from any policy blocks.
	if result.Allow {
		t.Error("expected Allow=false: one policy denies, result must be denied")
	}
}

func TestEngine_PolicyTypeSeparation(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	// Load a post_plan policy that denies.
	if err := e.Load(ctx, "p1", "deny-destroy", policy.TypePostPlan, denyDestroyPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Evaluating a different type should return no matching policies → allow.
	result, err := e.Evaluate(ctx, policy.TypePreApply, map[string]any{
		"resource_changes": []any{
			map[string]any{"change": map[string]any{"actions": []any{"delete"}}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Allow {
		t.Error("expected Allow=true: no pre_apply policies loaded")
	}
}

func TestEngine_Unload(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	if err := e.Load(ctx, "p1", "deny-destroy", policy.TypePostPlan, denyDestroyPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	e.Unload("p1")

	result, err := e.Evaluate(ctx, policy.TypePostPlan, map[string]any{
		"resource_changes": []any{
			map[string]any{"change": map[string]any{"actions": []any{"delete"}}},
		},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Allow {
		t.Error("expected Allow=true after policy unloaded")
	}
}

func TestEngine_LoginPolicy_DenyNoGroups(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	if err := e.Load(ctx, "p1", "login", policy.TypeLogin, loginPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := e.Evaluate(ctx, policy.TypeLogin, map[string]any{
		"email":  "user@example.com",
		"groups": []any{},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if result.Allow {
		t.Error("expected Allow=false for user with no groups")
	}
}

func TestEngine_LoginPolicy_AllowWithGroups(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	if err := e.Load(ctx, "p1", "login", policy.TypeLogin, loginPolicy); err != nil {
		t.Fatalf("Load: %v", err)
	}

	result, err := e.Evaluate(ctx, policy.TypeLogin, map[string]any{
		"email":  "user@example.com",
		"groups": []any{"engineering"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !result.Allow {
		t.Errorf("expected Allow=true for user with groups; denies: %v", result.Deny)
	}
}

func TestEngine_CompileError(t *testing.T) {
	ctx := context.Background()
	e := policy.NewEngine()

	err := e.Load(ctx, "p1", "bad", policy.TypePostPlan, `this is not valid rego !!!`)
	if err == nil {
		t.Error("expected compile error for invalid Rego, got nil")
	}
}
