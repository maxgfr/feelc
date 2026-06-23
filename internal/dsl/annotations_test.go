package dsl_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/dsl"
)

func TestAnnotationsAttach(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
@title "Annual income"
@question "What is your yearly income?"
@source "Article L1"
input income : number >= 0

@title "Eligibility"
@doc "Decides whether the applicant qualifies."
@source "Article L2"
decision eligible : boolean {
  needs: income
  hit: first
  >= 1000 => true
  -       => false
}`)
	if err != nil {
		t.Fatal(err)
	}
	in := m.Inputs[0]
	if in.Meta.Title != "Annual income" || in.Meta.Question != "What is your yearly income?" || in.Meta.Source != "Article L1" {
		t.Errorf("input meta = %+v", in.Meta)
	}
	d := m.Decisions[0]
	if d.Meta.Title != "Eligibility" || d.Meta.Doc != "Decides whether the applicant qualifies." || d.Meta.Source != "Article L2" {
		t.Errorf("decision meta = %+v", d.Meta)
	}
}

func TestAnnotationDangling(t *testing.T) {
	_, err := dsl.Parse(`model "m" {}
@title "orphan"
type T = context { a: number }`)
	if err == nil || !strings.Contains(err.Error(), "annotation") {
		t.Fatalf("dangling annotation must error, got %v", err)
	}
}

func TestAnnotationUnknownKey(t *testing.T) {
	_, err := dsl.Parse(`model "m" {}
@frobnicate "x"
input n : number`)
	if err == nil || !strings.Contains(err.Error(), "unknown annotation") {
		t.Fatalf("unknown annotation must error, got %v", err)
	}
}

func TestAnnotationRequiresQuotedValue(t *testing.T) {
	_, err := dsl.Parse(`model "m" {}
@title bare
input n : number`)
	if err == nil {
		t.Fatalf("annotation without a quoted value must error")
	}
}
