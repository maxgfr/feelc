package project

import (
	"strings"
	"testing"
)

func TestRetrieveContextTargetAndBindings(t *testing.T) {
	p, err := Load("testdata/crossmod")
	if err != nil {
		t.Fatal(err)
	}
	ctx := p.RetrieveContext("approve a loan based on amount and kyc result", "loan", 0)

	// The target module's source is included for editing.
	if !strings.Contains(ctx, "Target module to edit: loan") {
		t.Error("missing target-module header")
	}
	if !strings.Contains(ctx, "decision approved") {
		t.Error("target module source not included")
	}
	// Its cross-module binding is surfaced as an input alias.
	if !strings.Contains(ctx, "kyc_ok") || !strings.Contains(ctx, "kyc.passed") {
		t.Errorf("missing cross-module binding hint; got:\n%s", ctx)
	}
	// The other module's signature is present (reference only).
	if !strings.Contains(ctx, "module kyc") || !strings.Contains(ctx, "decision kyc.passed") {
		t.Error("missing other-module signature")
	}
	// The target module must not appear in the "other modules" list.
	if strings.Contains(ctx, "### module loan") {
		t.Error("target module leaked into the other-modules signatures")
	}
}

func TestRetrieveContextRanksByOverlapAndCapsK(t *testing.T) {
	p, err := Load("testdata/tables") // modules: pricing, flags
	if err != nil {
		t.Fatal(err)
	}
	// Query mentions the pricing concept; pricing should rank above flags when editing neither.
	ctx := p.RetrieveContext("compute the surcharge tier from amount and threshold", "flags", 1)
	if !strings.Contains(ctx, "module pricing") {
		t.Errorf("expected the lexically-relevant module (pricing) in top-K; got:\n%s", ctx)
	}
	// k=1 means at most one "other module" signature section beyond the target.
	if n := strings.Count(ctx, "### module "); n > 1 {
		t.Errorf("k=1 should yield at most 1 other-module signature, got %d", n)
	}
}

func TestRetrieveContextDeterministic(t *testing.T) {
	p, err := Load("testdata/crossmod")
	if err != nil {
		t.Fatal(err)
	}
	a := p.RetrieveContext("anything", "loan", 0)
	b := p.RetrieveContext("anything", "loan", 0)
	if a != b {
		t.Error("RetrieveContext is not deterministic")
	}
}

func TestTokenize(t *testing.T) {
	got := tokenize("Approve a LOAN, by amount >= 100!")
	for _, want := range []string{"approve", "loan", "amount", "100"} {
		if !got[want] {
			t.Errorf("tokenize missing %q (got %v)", want, got)
		}
	}
	if got["a"] || got[">="] {
		t.Error("tokenize kept a too-short / punctuation token")
	}
}
