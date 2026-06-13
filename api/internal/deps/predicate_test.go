// SPDX-License-Identifier: AGPL-3.0-or-later
package deps

import "testing"

func TestPredicate_Unset_AlwaysMatches(t *testing.T) {
	if !(Predicate{}).Matches(RunFields{}) {
		t.Error("empty predicate should match unconditionally")
	}
}

func TestPredicate_StringEquality(t *testing.T) {
	p := Predicate{Field: "type", Op: "==", Value: "tracked"}
	if !p.Matches(RunFields{Type: "tracked"}) {
		t.Error("'tracked' == 'tracked' should match")
	}
	if p.Matches(RunFields{Type: "destroy"}) {
		t.Error("'destroy' == 'tracked' should not match")
	}
}

func TestPredicate_IntOrdering(t *testing.T) {
	p := Predicate{Field: "plan_change", Op: ">", Value: "0"}
	cases := []struct {
		change int
		want   bool
	}{{0, false}, {1, true}, {-1, false}, {100, true}}
	for _, c := range cases {
		if got := p.Matches(RunFields{PlanChange: c.change}); got != c.want {
			t.Errorf("plan_change=%d: got %v want %v", c.change, got, c.want)
		}
	}
}

func TestPredicate_FloatComparison(t *testing.T) {
	p := Predicate{Field: "cost_change", Op: ">=", Value: "10.5"}
	if !p.Matches(RunFields{CostChange: 10.5}) {
		t.Error("10.5 >= 10.5 should match (boundary)")
	}
	if !p.Matches(RunFields{CostChange: 100}) {
		t.Error("100 >= 10.5 should match")
	}
	if p.Matches(RunFields{CostChange: 10.4}) {
		t.Error("10.4 >= 10.5 should not match")
	}
}

func TestPredicate_Boolean(t *testing.T) {
	p := Predicate{Field: "is_drift", Op: "==", Value: "true"}
	if !p.Matches(RunFields{IsDrift: true}) {
		t.Error("is_drift=true matches")
	}
	if p.Matches(RunFields{IsDrift: false}) {
		t.Error("is_drift=false should not match true predicate")
	}
}

func TestPredicate_Validate(t *testing.T) {
	cases := []struct {
		name    string
		p       Predicate
		wantErr bool
	}{
		{"empty is fine", Predicate{}, false},
		{"valid string ==", Predicate{Field: "type", Op: "==", Value: "tracked"}, false},
		{"valid int >", Predicate{Field: "plan_change", Op: ">", Value: "0"}, false},
		{"valid float", Predicate{Field: "cost_change", Op: ">=", Value: "5.50"}, false},
		{"valid bool", Predicate{Field: "is_drift", Op: "==", Value: "true"}, false},
		{"unknown field", Predicate{Field: "outputs.foo", Op: "==", Value: "bar"}, true},
		{"unknown op", Predicate{Field: "type", Op: "~=", Value: "x"}, true},
		{"non-numeric on int", Predicate{Field: "plan_add", Op: ">", Value: "lots"}, true},
		{"non-numeric on float", Predicate{Field: "cost_change", Op: ">", Value: "lots"}, true},
		{"ordering on string", Predicate{Field: "type", Op: ">", Value: "tracked"}, true},
		{"ordering on bool", Predicate{Field: "is_drift", Op: ">", Value: "true"}, true},
		{"bool non-bool value", Predicate{Field: "is_drift", Op: "==", Value: "yes"}, true},
		{"partial predicate (missing op)", Predicate{Field: "type", Value: "tracked"}, false}, // IsSet returns false → valid as 'unset'
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.p.Validate()
			if (err != nil) != c.wantErr {
				t.Errorf("got err=%v want err=%v", err, c.wantErr)
			}
		})
	}
}
