package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
)

const applicabilitySrc = `model "benefits" {}
input income : number >= 0
input is_student : boolean
decision housing_aid : number {
  = 200
  applicable if income < 1500
}
decision student_aid : number {
  = 150
  applicable if is_student
}
decision total_aid : number = housing_aid + student_aid`

func TestApplicabilityPropagation(t *testing.T) {
	m, err := dsl.Parse(applicabilitySrc)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		income     int
		student    bool
		decision   string
		wantOutput string // JSON-encoded canon
	}{
		{900, true, "total_aid", `"350"`},              // both apply: 200 + 150
		{900, false, "total_aid", `"200"`},             // student NA acts as 0 in the sum
		{2000, true, "total_aid", `"150"`},             // housing NA acts as 0
		{2000, false, "total_aid", `"non-applicable"`}, // both NA -> sum is non-applicable
		{2000, false, "housing_aid", `"non-applicable"`},
		{900, false, "housing_aid", `"200"`},
	}
	for _, c := range cases {
		out, err := engine.Eval(cm, c.decision, map[string]any{"income": c.income, "is_student": c.student})
		if err != nil {
			t.Fatalf("%s(income=%d,student=%v): %v", c.decision, c.income, c.student, err)
		}
		b, _ := json.Marshal(canon(out))
		if string(b) != c.wantOutput {
			t.Errorf("%s(income=%d,student=%v) = %s, want %s", c.decision, c.income, c.student, b, c.wantOutput)
		}
	}
}

// ADR 0013: a non-applicable value reaching a comparison must be a LOUD error, not a silent boolean.
func TestApplicabilityComparisonErrors(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input income : number >= 0
decision aid : number {
  = 200
  applicable if income < 1500
}
decision big : boolean = aid > 100`)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	// income >= 1500 -> aid is non-applicable -> the comparison `aid > 100` must error.
	if _, err := engine.Eval(cm, "big", map[string]any{"income": 2000}); err == nil {
		t.Fatal("comparing a non-applicable value must error (ADR 0013), got nil")
	}
	// income < 1500 -> aid = 200 -> comparison is fine.
	out, err := engine.Eval(cm, "big", map[string]any{"income": 900})
	if err != nil || out != true {
		t.Fatalf("big(900) = %v, %v; want true", out, err)
	}
}

func TestApplicabilityRejectedOnTable(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input x : number in [0..10]
decision d : string {
  needs: x
  applicable if x > 0
  hit: first
  < 5 => "lo"
  -   => "hi"
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := compiler.Compile(m); err == nil || !strings.Contains(err.Error(), "applicable if") {
		t.Fatalf("applicable-if on a table must be rejected, got %v", err)
	}
}
