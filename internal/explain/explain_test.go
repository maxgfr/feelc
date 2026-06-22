package explain_test

import (
	"os"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/ir"
)

func loadCredit(t *testing.T) *ir.CompiledModel {
	t.Helper()
	b, err := os.ReadFile("../../examples/credit/credit.rules")
	if err != nil {
		t.Fatal(err)
	}
	m, err := dsl.Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

func cell(tr *explain.Trace, input string) *struct {
	Src   string
	Value string
} {
	for _, c := range tr.Cells {
		if c.Input == input {
			return &struct {
				Src   string
				Value string
			}{c.Src, c.Value}
		}
	}
	return nil
}

// The winning rule and the justifying cell of a rejection (insufficient score) are surfaced,
// along with the source position and the column value.
func TestExplainCreditRejectedLowScore(t *testing.T) {
	cm := loadCredit(t)
	tr, err := explain.Explain(cm, "eligibility", map[string]any{
		"credit_score": 500, "annual_income": 60000, "monthly_debt": 1500, "age": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Matched || tr.Fallback {
		t.Fatalf("expected a match (no fallback), trace=%+v", tr)
	}
	if tr.HitPolicy != "first" {
		t.Errorf("hitPolicy = %q, expected first", tr.HitPolicy)
	}
	out, ok := tr.Output.(map[string]any)
	if !ok || out["reason"] != "insufficient score" || out["eligible"] != false {
		t.Errorf("output = %+v, expected eligible=false reason=\"insufficient score\"", tr.Output)
	}
	c := cell(tr, "credit_score")
	if c == nil {
		t.Fatalf("justifying cell credit_score missing, cells=%+v", tr.Cells)
	}
	if c.Value != "500" {
		t.Errorf("credit_score value = %q, expected 500", c.Value)
	}
	if c.Src == "" {
		t.Errorf("the justifying cell must carry its source text (Src)")
	}
}

// Approved case: the winning rule differs.
func TestExplainCreditApproved(t *testing.T) {
	cm := loadCredit(t)
	tr, err := explain.Explain(cm, "eligibility", map[string]any{
		"credit_score": 720, "annual_income": 60000, "monthly_debt": 1500, "age": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := tr.Output.(map[string]any)
	if out["eligible"] != true || out["reason"] != "approved" {
		t.Errorf("output = %+v, expected eligible=true reason=\"approved\"", tr.Output)
	}
}

// Regression (adversarial review): under FIRST, Trace must SHORT-CIRCUIT like Eval. Rule 1
// (geometric) matches for x=0; rule 2 has a cell Op=Prog that errors (division by zero).
// Eval returns "zero" without touching rule 2 -> Trace must NOT error either.
func TestExplainFirstShortCircuitsBeforeErroringRule(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input x : number
decision d : string {
  needs: x
  hit: first
  0           => "zero"
  100 / ? = 0 => "never"
}`)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := explain.Explain(cm, "d", map[string]any{"x": 0})
	if err != nil {
		t.Fatalf("Trace errored where Eval succeeds (FIRST divergence): %v", err)
	}
	if tr.RuleIndex != 1 || tr.Output != "zero" {
		t.Errorf("expected rule #1 -> \"zero\", got #%d -> %v", tr.RuleIndex, tr.Output)
	}
}

// A literal-expression decision (dti) is marked 'not geometric' without lying, with its source.
func TestExplainLiteralExprIsHonest(t *testing.T) {
	cm := loadCredit(t)
	tr, err := explain.Explain(cm, "dti", map[string]any{
		"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Kind != "literal-expr" {
		t.Errorf("kind = %q, expected literal-expr", tr.Kind)
	}
	if !tr.NotGeometric {
		t.Errorf("an expression must be marked not geometric (honesty)")
	}
	if tr.ExprSrc == "" {
		t.Errorf("the expression source (ExprSrc) must be set")
	}
}
