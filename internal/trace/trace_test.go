package trace_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/trace"
)

func compile(t *testing.T, src string) *ir.CompiledModel {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

const billing = `model "billing" {}
input amount : number >= 0
@source "Pricing policy section 2: standard discount"
decision discount : number {
  needs: amount
  hit: first
  >= 100 => 0.1
  -      => 0
}
decision total : number = amount - amount * discount`

// Build reports each decision's @source and flags decisions that cite no source.
func TestBuildRuleSide(t *testing.T) {
	cm := compile(t, billing)
	r := trace.Build(cm)

	if r.Coverage.Decisions != 2 || r.Coverage.DecisionsSourced != 1 {
		t.Errorf("coverage = %+v, want 2 decisions / 1 sourced", r.Coverage)
	}
	if strings.Join(r.Untraced, ",") != "total" {
		t.Errorf("untraced = %v, want [total]", r.Untraced)
	}
	var discount *trace.DecisionTrace
	for i := range r.Decisions {
		if r.Decisions[i].Decision == "discount" {
			discount = &r.Decisions[i]
		}
	}
	if discount == nil {
		t.Fatal("discount decision missing from report")
	}
	if discount.Source != "Pricing policy section 2: standard discount" {
		t.Errorf("discount source = %q", discount.Source)
	}
	if discount.Kind != "table" {
		t.Errorf("discount kind = %q, want table", discount.Kind)
	}
}

// BuildWithSource heuristically marks which source paragraphs are referenced by a decision's
// @source citation. The pricing paragraph is covered by `discount`; the unrelated refund
// paragraph is not (the heuristic is conservative — no false positives).
func TestBuildWithSourceCoverage(t *testing.T) {
	cm := compile(t, billing)
	spec := `Pricing policy section 2: standard discount applies to large orders.

Refund policy section 9: returns within 30 days.`
	r := trace.BuildWithSource(cm, []byte(spec))

	if r.Coverage.SpansTotal != 2 || r.Coverage.SpansCovered != 1 {
		t.Fatalf("span coverage = %+v, want 2 total / 1 covered", r.Coverage)
	}
	var covered, uncovered *trace.SpanCoverage
	for i := range r.Spans {
		if strings.Contains(strings.ToLower(r.Spans[i].Span), "pricing") {
			covered = &r.Spans[i]
		}
		if strings.Contains(strings.ToLower(r.Spans[i].Span), "refund") {
			uncovered = &r.Spans[i]
		}
	}
	if covered == nil || !covered.Covered || strings.Join(covered.By, ",") != "discount" {
		t.Errorf("pricing span must be covered by [discount], got %+v", covered)
	}
	if uncovered == nil || uncovered.Covered {
		t.Errorf("refund span must be uncovered, got %+v", uncovered)
	}
}
