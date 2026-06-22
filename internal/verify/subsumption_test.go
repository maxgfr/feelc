package verify_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/verify"
)

// Subsumption: rule #2 (>= 50) included in #1 (>= 0) with the SAME output -> redundant.
// Under ANY, this overlap with identical outputs is NOT a conflict; it was therefore
// silent before. Subsumption flags it (removable).
func TestVerifyDetectsRedundantSubsumedRule(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision d : string {
  needs: x
  hit: any
  >= 0  => "a"
  >= 50 => "a"
}`))
	f := has(rep, verify.KindSubsumed)
	if f == nil {
		t.Fatalf("subsumption expected (rule #2 included in #1, same output). Findings: %+v", rep.Findings)
	}
	if len(f.Rules) == 0 || f.Rules[0] != 2 {
		t.Errorf("redundant rule expected = #2, got %+v", f.Rules)
	}
}

// Disjoint rules: no subsumption.
func TestVerifyNoSubsumptionWhenDisjoint(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision d : string {
  needs: x
  hit: any
  [0..50)   => "a"
  [50..100] => "b"
}`))
	if f := has(rep, verify.KindSubsumed); f != nil {
		t.Errorf("no subsumption expected (disjoint rules), got %+v", f)
	}
}

// Subsumption does NOT duplicate an already reported conflict: under UNIQUE, the overlap is already
// a conflict, so we do not re-emit KindSubsumed.
func TestVerifyNoSubsumptionUnderUnique(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision d : string {
  needs: x
  hit: unique
  >= 0  => "a"
  >= 50 => "a"
}`))
	if f := has(rep, verify.KindSubsumed); f != nil {
		t.Errorf("UNIQUE: no subsumption (already a conflict), got %+v", f)
	}
	if has(rep, verify.KindConflict) == nil {
		t.Errorf("UNIQUE: an overlap conflict was expected")
	}
}
