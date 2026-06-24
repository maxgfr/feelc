package ir_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/ir"
)

// A null or non-applicable value satisfies NO cell, including `!= x` (ADR 0003: "null satisfies no
// condition"; ADR 0013: "NA behaves like null in matching"). Regression for a bug where the OpNe path
// returned !ValueEq(null, lit) == true, spuriously matching `!= x` on null/NA — the opposite of the
// invariant and inconsistent with the equivalent `not(= x)` negation path.
func TestMatchCell_NotEqual_NullAndNADoNotMatch(t *testing.T) {
	ne := ir.CellTest{Op: ir.OpNe, A: ir.Str("y")}
	notEq := ir.CellTest{Op: ir.OpEq, A: ir.Str("y"), Negate: true} // semantically identical to `!= "y"`

	cases := []struct {
		name string
		v    ir.Value
		want bool
	}{
		{"null vs != y", ir.Null(), false},
		{"NA vs != y", ir.NA(), false},
		{"x vs != y (differs)", ir.Str("x"), true},
		{"y vs != y (equal)", ir.Str("y"), false},
	}
	for _, c := range cases {
		got, err := ir.MatchCell(ne, c.v)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if got != c.want {
			t.Errorf("%s: MatchCell(!= y) = %v, want %v", c.name, got, c.want)
		}
		// `!= y` and `not(= y)` must agree on every value (especially null/NA).
		gotNot, err := ir.MatchCell(notEq, c.v)
		if err != nil {
			t.Fatalf("%s: not(= y) error: %v", c.name, err)
		}
		if gotNot != got {
			t.Errorf("%s: `!= y` (%v) and `not(= y)` (%v) disagree", c.name, got, gotNot)
		}
	}
}
