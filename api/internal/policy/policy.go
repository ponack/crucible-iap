// SPDX-License-Identifier: AGPL-3.0-or-later
// Package policy evaluates OPA/Rego policies embedded in the API server.
// Policies are compiled once at load time and evaluated per request in microseconds.
package policy

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/v1/rego"
	"github.com/open-policy-agent/opa/v1/topdown"
)

// Type identifies which hook a policy applies to.
type Type string

const (
	TypePrePlan    Type = "pre_plan"
	TypePostPlan   Type = "post_plan"
	TypePreApply   Type = "pre_apply"
	TypeTrigger    Type = "trigger"
	TypeLogin      Type = "login"
	TypeApproval   Type = "approval"
	TypeValidation Type = "validation"
)

// Result is the output of evaluating a policy against an input.
type Result struct {
	Allow          bool     `json:"allow"`
	Deny           []string `json:"deny,omitempty"`    // denial messages from the policy
	Warn           []string `json:"warn,omitempty"`    // warnings (non-blocking)
	Trigger        []string `json:"trigger,omitempty"` // stack IDs to trigger (trigger policies)
	RequireApproval bool    `json:"require_approval,omitempty"` // approval policies
}

// EvalRecord is one policy's evaluation output, for persisting to run_policy_results.
type EvalRecord struct {
	PolicyID   string
	PolicyName string
	PolicyType Type
	Result     Result
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
	rs, err := q.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return Result{}, err
	}
	return parseResultSet(rs), nil
}

// EvaluateSourceWithTrace is like EvaluateSource but also returns OPA's evaluation
// trace, formatted by topdown.PrettyTrace. Used by the policy test playground.
func (e *Engine) EvaluateSourceWithTrace(ctx context.Context, t Type, source string, input map[string]any) (Result, string, error) {
	q, err := rego.New(
		rego.Query(queryForType(t)),
		rego.Module("test.rego", source),
	).PrepareForEval(ctx)
	if err != nil {
		return Result{}, "", fmt.Errorf("compile: %w", err)
	}

	buf := topdown.NewBufferTracer()
	rs, err := q.Eval(ctx, rego.EvalInput(input), rego.EvalQueryTracer(buf))
	if err != nil {
		return Result{}, "", err
	}

	var sb strings.Builder
	topdown.PrettyTrace(&sb, *buf)
	return parseResultSet(rs), sb.String(), nil
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
		if r.RequireApproval {
			merged.RequireApproval = true
		}
	}
	if len(merged.Deny) > 0 {
		merged.Allow = false
	}
	return merged, nil
}

// EvaluateByIDs evaluates only the policies whose IDs are in the given set,
// returning both a merged result and per-policy records for persisting.
func (e *Engine) EvaluateByIDs(ctx context.Context, ids []string, input map[string]any) (Result, []EvalRecord, error) {
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}

	e.mu.RLock()
	var matching []*Policy
	for _, p := range e.policies {
		if idSet[p.ID] {
			matching = append(matching, p)
		}
	}
	e.mu.RUnlock()

	merged := Result{Allow: true}
	records := make([]EvalRecord, 0, len(matching))
	for _, p := range matching {
		r, err := p.eval(ctx, input)
		if err != nil {
			return Result{}, nil, fmt.Errorf("evaluate policy %s: %w", p.Name, err)
		}
		records = append(records, EvalRecord{PolicyID: p.ID, PolicyName: p.Name, PolicyType: p.Type, Result: r})
		merged.Deny = append(merged.Deny, r.Deny...)
		merged.Warn = append(merged.Warn, r.Warn...)
		merged.Trigger = append(merged.Trigger, r.Trigger...)
		if r.RequireApproval {
			merged.RequireApproval = true
		}
	}
	if len(merged.Deny) > 0 {
		merged.Allow = false
	}
	return merged, records, nil
}

func (p *Policy) eval(ctx context.Context, input map[string]any) (Result, error) {
	rs, err := p.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return Result{}, err
	}
	return parseResultSet(rs), nil
}

func parseResultSet(rs rego.ResultSet) Result {
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return Result{Allow: true}
	}
	val, ok := rs[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return Result{Allow: true}
	}
	result := Result{
		Allow:           true,
		Deny:            extractStrings(val, "deny"),
		Warn:            extractStrings(val, "warn"),
		Trigger:         extractStrings(val, "trigger"),
		RequireApproval: extractBool(val, "require_approval"),
	}
	if len(result.Deny) > 0 {
		result.Allow = false
	}
	return result
}

func extractStrings(val map[string]any, key string) []string {
	items, ok := val[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func extractBool(val map[string]any, key string) bool {
	b, ok := val[key].(bool)
	return ok && b
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
	case TypeApproval:
		return "data.crucible.approval"
	case TypeValidation:
		return "data.crucible.validation"
	default:
		return "data.crucible.plan"
	}
}
