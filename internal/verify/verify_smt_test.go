//go:build smt

package verify_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/verify"
)

func z3Present() bool { _, err := exec.LookPath("z3"); return err == nil }

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

// floor(?) is now ENCODABLE (to_int). Without a catch-all rule the table has a gap that the SMT
// backend PROVES (sat ⇒ uncovered input); with z3 absent it degrades honestly.
func TestSMTCompletenessGapFloor(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
input other : number in [0..100]
decision d : string {
  needs: x
  hit: first
  floor(?) < other => "lo"
}`))
	if z3Present() {
		if has(rep, verify.KindGap) == nil {
			t.Fatalf("expected an SMT-proven gap, got %+v", rep.Findings)
		}
	} else {
		assertZ3Missing(t, rep)
	}
}

// if/then/else (now allowed in cells, compiled to jumps) is re-encoded as `ite`; with a default
// the table is PROVEN complete.
func TestSMTCompletenessProvenIf(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
input other : number in [0..100]
decision d : string {
  needs: x
  hit: first
  if ? > 50 then true else false => "hi"
  -                              => "lo"
}`))
	if z3Present() {
		if has(rep, verify.KindGap) != nil {
			t.Fatalf("table is complete (default present), unexpected gap: %+v", rep.Findings)
		}
		if f := has(rep, verify.KindNotVerifiable); f == nil || !strings.Contains(f.Message, "PROVEN by SMT") {
			t.Fatalf("expected an SMT completeness proof, got %+v", rep.Findings)
		}
	} else {
		assertZ3Missing(t, rep)
	}
}

// UNIQUE with two OVERLAPPING Op=Prog rules: the SMT conflict query proves the overlap.
func TestSMTConflictUnique(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
input other : number in [0..100]
decision d : string {
  needs: x
  hit: unique
  floor(?) < other => "a"
  ? < other        => "b"
  -                => "c"
}`))
	if z3Present() {
		if has(rep, verify.KindConflict) == nil {
			t.Fatalf("expected an SMT-proven conflict, got %+v", rep.Findings)
		}
	} else {
		assertZ3Missing(t, rep)
	}
}

// UNIQUE with two DISJOINT Op=Prog rules that tile the domain: no conflict and no gap — both
// residuals cleared by SMT.
func TestSMTNoConflictDisjoint(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
input other : number in [0..100]
decision d : string {
  needs: x
  hit: unique
  ? < other  => "lo"
  ? >= other => "hi"
}`))
	if z3Present() {
		if has(rep, verify.KindConflict) != nil {
			t.Fatalf("rules are disjoint, unexpected conflict: %+v", rep.Findings)
		}
		if has(rep, verify.KindGap) != nil {
			t.Fatalf("rules tile the domain, unexpected gap: %+v", rep.Findings)
		}
	} else {
		assertZ3Missing(t, rep)
	}
}

func assertZ3Missing(t *testing.T, rep *verify.Report) {
	t.Helper()
	f := has(rep, verify.KindNotVerifiable)
	if f == nil || !strings.Contains(f.Message, "z3 not found") {
		t.Fatalf("z3 absent: expected honest 'z3 not found' degradation, got %+v", rep.Findings)
	}
}
