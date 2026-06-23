package project

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// TestCrossModuleBindingResolves loads a project where `loan` binds its `kyc_ok` input to `kyc.passed`
// and confirms: the bound input is omitted from the merged external inputs, the reference is redirected
// to the upstream qualified decision, and evaluation resolves transitively through the merged model.
func TestCrossModuleBindingResolves(t *testing.T) {
	p, err := Load("testdata/crossmod")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// External inputs: kyc__score and loan__amount survive; the bound loan__kyc_ok does NOT (it is wired).
	for _, want := range []string{"kyc__score", "loan__amount"} {
		if _, ok := p.Merged.Inputs[want]; !ok {
			t.Errorf("merged inputs missing %q", want)
		}
	}
	if _, ok := p.Merged.Inputs["loan__kyc_ok"]; ok {
		t.Error("bound input loan__kyc_ok leaked into merged external inputs")
	}

	// loan__approved now depends on the upstream kyc__passed (the cross-module edge).
	d, ok := p.Merged.Decision("loan__approved")
	if !ok {
		t.Fatal("missing loan__approved")
	}
	if !containsStr(d.Deps, "kyc__passed") {
		t.Errorf("loan__approved deps = %v, want to contain kyc__passed", d.Deps)
	}
	if containsStr(d.Deps, "loan__kyc_ok") {
		t.Errorf("loan__approved still depends on the unbound loan__kyc_ok: %v", d.Deps)
	}
	// The table column was redirected too.
	if !containsStr(d.Table.Inputs, "kyc__passed") {
		t.Errorf("loan__approved table.Inputs = %v, want to contain kyc__passed", d.Table.Inputs)
	}

	// End-to-end: provide only the real external inputs; kyc__passed is computed, loan__approved follows.
	out, err := engine.Eval(p.Merged, "loan__approved", map[string]any{"kyc__score": 700, "loan__amount": 50000})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if out != true {
		t.Errorf("approved(score=700, amount=50000) = %v, want true", out)
	}
	// Failing KYC propagates: score below 600 → kyc__passed false → not approved.
	out, _ = engine.Eval(p.Merged, "loan__approved", map[string]any{"kyc__score": 500, "loan__amount": 50000})
	if out != false {
		t.Errorf("approved(score=500) = %v, want false (kyc failed)", out)
	}
}

func TestCrossModuleDanglingAliasRejected(t *testing.T) {
	_, err := Load("testdata/crossmod-dangling")
	if err == nil {
		t.Fatal("expected an error for a `uses` alias pointing at a nonexistent decision")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error %q should mention the missing decision", err)
	}
}

func TestCrossModuleCycleRejected(t *testing.T) {
	_, err := Load("testdata/crossmod-cycle")
	if err == nil {
		t.Fatal("expected an error for a cross-module dependency cycle")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Errorf("error %q should report a cyclic dependency", err)
	}
}
