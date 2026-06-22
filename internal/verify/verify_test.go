package verify_test

import (
	"os"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/verify"
)

func compile(t *testing.T, src string) *ir.CompiledModel {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cm
}

func has(rep *verify.Report, kind verify.Kind) *verify.Finding {
	for i := range rep.Findings {
		if rep.Findings[i].Kind == kind {
			return &rep.Findings[i]
		}
	}
	return nil
}

// The credit example is PROVEN complete and conflict-free (FIRST); its `default` line is useless.
func TestVerifyCreditIsComplete(t *testing.T) {
	b, err := os.ReadFile("../../examples/credit/credit.rules")
	if err != nil {
		t.Fatal(err)
	}
	rep := verify.Verify(compile(t, string(b)))
	if n := rep.Blockers(); n != 0 {
		t.Errorf("credit: %d blockers, expected 0 (table proven complete). Findings: %+v", n, rep.Findings)
	}
	if has(rep, verify.KindGap) != nil {
		t.Errorf("credit: unexpected completeness gap. Findings: %+v", rep.Findings)
	}
	if has(rep, verify.KindUnreachableDefault) == nil {
		t.Errorf("credit: the `default` line should be detected as useless (complete table)")
	}
}

// Completeness gap: [30..60) not covered, no default -> error + counterexample.
func TestVerifyDetectsGap(t *testing.T) {
	rep := verify.Verify(compile(t, `model "g" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: first
     [0..30)   => "low"
     [60..100] => "high"
}`))
	g := has(rep, verify.KindGap)
	if g == nil {
		t.Fatalf("gap expected, findings: %+v", rep.Findings)
	}
	if g.Severity != verify.SevError {
		t.Errorf("gap without default -> error severity, got %s", g.Severity)
	}
	if g.Witness["n"] == "" {
		t.Errorf("counterexample expected for n, got %+v", g.Witness)
	}
}

// UNIQUE with overlap -> blocking conflict.
func TestVerifyDetectsUniqueConflict(t *testing.T) {
	rep := verify.Verify(compile(t, `model "u" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: unique
     >= 0  => "a"
     >= 50 => "b"
}`))
	c := has(rep, verify.KindConflict)
	if c == nil || c.Severity != verify.SevError {
		t.Fatalf("blocking UNIQUE conflict expected, findings: %+v", rep.Findings)
	}
}

// FIRST: a rule whose every case is already covered by an earlier rule is masked.
func TestVerifyDetectsMaskedRule(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: first
     >= 0  => "all"
     >= 50 => "fifty"
}`))
	if has(rep, verify.KindDeadRule) == nil {
		t.Errorf("masked rule expected, findings: %+v", rep.Findings)
	}
}

// Honest degradation: an Op=Prog cell makes the table not geometrically provable.
func TestVerifyDegradesOnProgCell(t *testing.T) {
	rep := verify.Verify(compile(t, `model "p" {}
input a : number
input b : number
decision d : string {
  needs: a, b
  hit: first
     > b | - => "over"
     -   | - => "ok"
}`))
	if has(rep, verify.KindNotVerifiable) == nil {
		t.Errorf("'not verifiable' degradation expected (Op=Prog cell), findings: %+v", rep.Findings)
	}
}
