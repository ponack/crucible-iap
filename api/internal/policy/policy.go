// SPDX-License-Identifier: AGPL-3.0-or-later
// Package policy evaluates OPA/Rego policies embedded in the API server.
// Policies are compiled once at load time and evaluated per request in microseconds.
package policy

import (
	"context"
	"fmt"
	"sync"

	"github.com/open-policy-agent/opa/rego"
)

// Type identifies which hook a policy applies to.
type Type string

const (
	TypePrePlan  Type = "pre_plan"
	TypePostPlan Type = "post_plan"
	TypePreApply Type = "pre_apply"
	TypeTrigger  Type = "trigger"
	TypeLogin    Type = "login"
)

// Result is the output of evaluating a policy against an input.
type Result struct {
	Allow   bool     `json:"allow"`
	Deny    []string `json:"deny,omitempty"`    // denial messages from the policy
	Warn    []string `json:"warn,omitempty"`    // warnings (non-blocking)
	Trigger []string `json:"trigger,omitempty"` // stack IDs to trigger (trigger policies)
}

// Policy is a compiled, ready-to-evaluate Rego policy.
type Policy struct {
	ID     string
	Name   string
	Type   Type
	Source string
	query  rego.PreparedEvalQuery
}

// Engine holds all active compiled policies and evaluates them on demand.
type Engine struct {
	mu       sync.RWMutex
	policies map[string]*Policy // keyed by policy ID
}

func NewEngine() *Engine {
	return &Engine{policies: make(map[string]*Policy)}
}

// Load compiles and registers a policy. Replaces any existing policy with the same ID.
func (e *Engine) Load(ctx context.Context, id, name string, t Type, source string) error {
	q, err := rego.New(
		rego.Query(queryForType(t)),
		rego.Module(name+".rego", source),
	).PrepareForEval(ctx)
	if err != nil {
		return fmt.Errorf("compile policy %s: %w", name, err)
	}

	e.mu.Lock()
	e.policies[id] = &Policy{ID: id, Name: name, Type: t, Source: source, query: q}
	e.mu.Unlock()
	return nil
}

// EvaluateSource compiles source and evaluates it against input in one shot,
// without persisting the policy in the engine. Used for the dry-run sandbox.
func (e *Engine) EvaluateSource(ctx context.Context, t Type, source string, input map[string]any) (Result, error) {
	q, err := rego.New(
		rego.Query(queryForType(t)),
		rego.Module("test.rego", source),
	).PrepareForEval(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("compile: %w", err)
	}
	p := &Policy{query: q}
	return p.eval(ctx, input)
}

// Unload removes a policy by ID.
func (e *Engine) Unload(id string) {
	e.mu.Lock()
	delete(e.policies, id)
	e.mu.Unlock()
}

// Evaluate runs all policies of the given type against the input and merges results.
// A single deny from any policy blocks the operation.
func (e *Engine) Evaluate(ctx context.Context, t Type, input map[string]any) (Result, error) {
	e.mu.RLock()
	var matching []*Policy
	for _, p := range e.policies {
		if p.Type == t {
			matching = append(matching, p)
		}
	}
	e.mu.RUnlock()

	merged := Result{Allow: true}
	for _, p := range matching {
		r, err := p.eval(ctx, input)
		if err != nil {
			return Result{}, fmt.Errorf("evaluate policy %s: %w", p.Name, err)
		}
		merged.Deny = append(merged.Deny, r.Deny...)
		merged.Warn = append(merged.Warn, r.Warn...)
		merged.Trigger = append(merged.Trigger, r.Trigger...)
	}
	if len(merged.Deny) > 0 {
		merged.Allow = false
	}
	return merged, nil
}

func (p *Policy) eval(ctx context.Context, input map[string]any) (Result, error) {
	rs, err := p.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return Result{}, err
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return Result{Allow: true}, nil
	}

	// Rego policies return a set; we expect deny/warn/trigger keys
	val, ok := rs[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return Result{Allow: true}, nil
	}

	result := Result{Allow: true}
	if deny, ok := val["deny"].([]any); ok {
		for _, d := range deny {
			if s, ok := d.(string); ok {
				result.Deny = append(result.Deny, s)
			}
		}
	}
	if warn, ok := val["warn"].([]any); ok {
		for _, w := range warn {
			if s, ok := w.(string); ok {
				result.Warn = append(result.Warn, s)
			}
		}
	}
	if trigger, ok := val["trigger"].([]any); ok {
		for _, t := range trigger {
			if s, ok := t.(string); ok {
				result.Trigger = append(result.Trigger, s)
			}
		}
	}
	if len(result.Deny) > 0 {
		result.Allow = false
	}
	return result, nil
}

func queryForType(t Type) string {
	switch t {
	case TypePostPlan:
		return "data.crucible.plan"
	case TypePreApply:
		return "data.crucible.apply"
	case TypeTrigger:
		return "data.crucible.trigger"
	case TypeLogin:
		return "data.crucible.login"
	default:
		return "data.crucible.plan"
	}
}
