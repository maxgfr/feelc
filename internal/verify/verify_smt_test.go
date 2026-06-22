//go:build smt

package verify_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/verify"
)

// Under `-tags smt`, the SMT backend is wired in: a table with an Op=Prog cell is routed to it
// (and no longer the generic "unverified residual"). Depending on the presence of z3: proof, gap, or
// honest degradation "z3 not found" — in all cases the message mentions SMT/z3.
func TestSMTBackendHandlesOpProg(t *testing.T) {
	// `? < other` references another input -> Op=Prog cell (non-geometric).
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
input other : number in [0..100]
decision d : string {
  needs: x
  hit: first
  ? < other => "lo"
  -         => "hi"
}`))
	f := has(rep, verify.KindNotVerifiable)
	g := has(rep, verify.KindGap)
	var msg string
	if f != nil {
		msg = f.Message
	} else if g != nil {
		msg = g.Message
	} else {
		t.Fatalf("expected a finding from the SMT backend, report=%+v", rep.Findings)
	}
	if !strings.Contains(msg, "SMT") && !strings.Contains(msg, "z3") {
		t.Errorf("the SMT backend should have handled Op=Prog; message=%q", msg)
	}
}
