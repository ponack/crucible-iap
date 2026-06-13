// SPDX-License-Identifier: AGPL-3.0-or-later

// Package-level evaluator for stack_dependencies trigger predicates.
// See migration 081 for the schema; see triggerDownstreamStacks for the
// integration point.
package deps

import (
	"errors"
	"strconv"
)

// RunFields is the subset of the upstream's just-finished run that
// predicates can reference. Kept tiny on purpose; broader access would
// re-open the "load state file from MinIO" can of worms.
//
// All numeric fields use plain `int` / `float64` since the runs table
// stores them as nullable but the trigger gate only runs after a
// successful apply, by which point plan counts are always present.
type RunFields struct {
	Type        string  // 'tracked' | 'destroy' | 'proposed'
	PlanAdd     int
	PlanChange  int
	PlanDestroy int
	CostChange  float64
	IsDrift     bool
}

// Predicate is a single-condition trigger guard on a stack_dependencies edge.
// All zero-value (field == "") means "no predicate" — caller should treat the
// edge as always-eligible.
type Predicate struct {
	Field string
	Op    string
	Value string
}

// IsSet returns true when the predicate has all three components populated.
// Used by callers to skip evaluation cleanly when the edge is unconfigured.
func (p Predicate) IsSet() bool {
	return p.Field != "" && p.Op != "" && p.Value != ""
}

// supportedFields is the closed set of run-field names a predicate may
// reference. Centralised here so the handler can use it to validate user
// input before persisting.
var supportedFields = map[string]bool{
	"type":         true,
	"plan_add":     true,
	"plan_change":  true,
	"plan_destroy": true,
	"cost_change":  true,
	"is_drift":     true,
}

var supportedOps = map[string]bool{
	"==": true, "!=": true,
	">": true, "<": true, ">=": true, "<=": true,
}

// Validate checks that the field is supported, the op is supported, the
// value parses for numeric / boolean fields, and string fields don't pair
// with ordering operators (>, <, etc.).
func (p Predicate) Validate() error {
	if !p.IsSet() {
		return nil // an empty predicate is valid — the edge just has no condition
	}
	if !supportedFields[p.Field] {
		return errors.New("trigger_when_field: unsupported (see docs for the field list)")
	}
	if !supportedOps[p.Op] {
		return errors.New("trigger_when_op: must be one of == != > < >= <=")
	}
	if v, ok := fieldValidators[p.Field]; ok {
		return v(p.Op, p.Value)
	}
	return nil
}

// fieldValidators dispatches per-field operand and operator checks. Kept
// as a map (rather than inline in Validate) so each field's rule sits
// next to its sibling and gocyclo doesn't trip on a wide switch.
var fieldValidators = map[string]func(op, value string) error{
	"plan_add":     validateIntValue,
	"plan_change":  validateIntValue,
	"plan_destroy": validateIntValue,
	"cost_change":  validateFloatValue,
	"is_drift":     validateBoolValue,
	"type":         validateStringEquality,
}

func validateIntValue(_, value string) error {
	if _, err := strconv.Atoi(value); err != nil {
		return errors.New("trigger_when_value: plan_* fields require an integer")
	}
	return nil
}

func validateFloatValue(_, value string) error {
	if _, err := strconv.ParseFloat(value, 64); err != nil {
		return errors.New("trigger_when_value: cost_change requires a number")
	}
	return nil
}

func validateBoolValue(op, value string) error {
	if op != "==" && op != "!=" {
		return errors.New("trigger_when_op: is_drift only supports == / !=")
	}
	if value != "true" && value != "false" {
		return errors.New("trigger_when_value: is_drift requires 'true' or 'false'")
	}
	return nil
}

func validateStringEquality(op, _ string) error {
	if op != "==" && op != "!=" {
		return errors.New("trigger_when_op: type only supports == / !=")
	}
	return nil
}

// Matches evaluates the predicate against the upstream's run fields.
// An unset predicate always matches (callers can rely on this so they
// don't have to branch around the no-predicate case at every callsite).
func (p Predicate) Matches(r RunFields) bool {
	if !p.IsSet() {
		return true
	}
	switch p.Field {
	case "type":
		return compareString(r.Type, p.Op, p.Value)
	case "plan_add":
		return compareInt(r.PlanAdd, p.Op, p.Value)
	case "plan_change":
		return compareInt(r.PlanChange, p.Op, p.Value)
	case "plan_destroy":
		return compareInt(r.PlanDestroy, p.Op, p.Value)
	case "cost_change":
		return compareFloat(r.CostChange, p.Op, p.Value)
	case "is_drift":
		want, _ := strconv.ParseBool(p.Value)
		switch p.Op {
		case "==":
			return r.IsDrift == want
		case "!=":
			return r.IsDrift != want
		}
	}
	return false
}

func compareString(got, op, want string) bool {
	switch op {
	case "==":
		return got == want
	case "!=":
		return got != want
	}
	return false // ordering ops on strings already rejected by Validate
}

func compareInt(got int, op, wantStr string) bool {
	want, err := strconv.Atoi(wantStr)
	if err != nil {
		return false
	}
	return compareOrderable(float64(got), op, float64(want))
}

func compareFloat(got float64, op, wantStr string) bool {
	want, err := strconv.ParseFloat(wantStr, 64)
	if err != nil {
		return false
	}
	return compareOrderable(got, op, want)
}

func compareOrderable(got float64, op string, want float64) bool {
	switch op {
	case "==":
		return got == want
	case "!=":
		return got != want
	case ">":
		return got > want
	case "<":
		return got < want
	case ">=":
		return got >= want
	case "<=":
		return got <= want
	}
	return false
}
