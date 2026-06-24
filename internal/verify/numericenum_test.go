package verify_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/verify"
)

// A numeric enum domain (`in {1,2,3}`) is a DISCRETE set: a table that covers every member is complete,
// and a rule matching only values outside the set is dead. Regression for a bug where the geometric
// verifier treated a numeric enum as a continuous range, fabricating midpoints/neighbours (1.5, 0, 4)
// that produced FALSE completeness gaps and HID genuinely dead rules.
func TestVerify_NumericEnumIsDiscrete(t *testing.T) {
	complete := `model "ne" {}
input n : number in {1,2,3}
decision d : string {
  needs: n
  hit: first
     1 => "a"
     2 => "b"
     3 => "c"
}`
	rep := verify.Verify(compile(t, complete))
	if g := has(rep, verify.KindGap); g != nil {
		t.Errorf("false gap on complete numeric-enum table: witness=%v", g.Witness)
	}

	// A rule matching a value outside the declared domain {1,2,3} is unreachable -> must be dead.
	deadRule := `model "ne" {}
input n : number in {1,2,3}
decision d : string {
  needs: n
  hit: first
     1 => "a"
     2 => "b"
     3 => "c"
     5 => "z"
}`
	rep = verify.Verify(compile(t, deadRule))
	if has(rep, verify.KindDeadRule) == nil {
		t.Errorf("rule `5 => z` is unreachable under domain {1,2,3} but was not flagged dead")
	}
}
