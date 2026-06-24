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

// The credit example is PROVEN complete over the declared domain and conflict-free (FIRST). Its
// `default` line must NOT be flagged useless: no rule is a catch-all, so a null/missing input on any
// column (ADR 0003 §2) falls through every rule to the default — the default is genuinely reachable.
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
	if f := has(rep, verify.KindUnreachableDefault); f != nil {
		t.Errorf("credit: the `default` line catches null inputs and must NOT be flagged unreachable. Findings: %+v", rep.Findings)
	}
}

// A FIRST table that tiles its whole domain with explicit rules but has NO catch-all rule must keep
// its `default` reachable for null/missing inputs (ADR 0003 §2-§3). Regression for the false
// "unreachable-default" that the OpNe null fix exposed (and that also hit idiomatic `< / >` tables).
func TestVerifyDefaultReachableForNull(t *testing.T) {
	// `!= x` form (the OpNe path) and the range-tiling form must both keep the default.
	for name, src := range map[string]string{
		"not-equal": `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: first
     5    => "five"
     != 5 => "other"
     default => "fallback"
}`,
		"range-tile": `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: first
     < 0  => "neg"
     >= 0 => "nonneg"
     default => "fallback"
}`,
	} {
		rep := verify.Verify(compile(t, src))
		if f := has(rep, verify.KindUnreachableDefault); f != nil {
			t.Errorf("%s: default catches null and must NOT be flagged unreachable; findings=%+v", name, rep.Findings)
		}
	}

	// A genuine catch-all (`-`) rule DOES make a following default unreachable — the guard must not
	// over-suppress.
	rep := verify.Verify(compile(t, `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: first
     - => "all"
     default => "fallback"
}`))
	if has(rep, verify.KindUnreachableDefault) == nil {
		t.Errorf("catch-all rule makes the default genuinely unreachable, but it was not flagged; findings=%+v", rep.Findings)
	}
}

// A HitPriority table whose rule produces an output absent from the `priority:` list must be flagged:
// that output ranks last and can silently lose to a listed value (GLUE-002).
func TestVerifyPriorityGap(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision g : string {
  needs: x
  hit: priority
  priority: "lo"
     >= 0 => "hi"
     >= 0 => "lo"
}`))
	f := has(rep, verify.KindPriorityGap)
	if f == nil {
		t.Fatalf("expected a priority-gap finding for unlisted output \"hi\", findings: %+v", rep.Findings)
	}
	if f.Severity != verify.SevWarning {
		t.Errorf("priority-gap should be a warning, got %s", f.Severity)
	}
	// Control: when every produced output is listed, no priority-gap.
	rep2 := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision g : string {
  needs: x
  hit: priority
  priority: "hi", "lo"
     >= 0 => "hi"
     >= 50 => "lo"
}`))
	if has(rep2, verify.KindPriorityGap) != nil {
		t.Errorf("all outputs are listed; no priority-gap expected, findings: %+v", rep2.Findings)
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
