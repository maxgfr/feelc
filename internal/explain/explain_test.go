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

// La règle gagnante et la cellule justifiante d'un refus (score insuffisant) sont remontées,
// avec la position source et la valeur de la colonne.
func TestExplainCreditRejectedLowScore(t *testing.T) {
	cm := loadCredit(t)
	tr, err := explain.Explain(cm, "eligibility", map[string]any{
		"credit_score": 500, "annual_income": 60000, "monthly_debt": 1500, "age": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !tr.Matched || tr.Fallback {
		t.Fatalf("attendu un match (pas de fallback), trace=%+v", tr)
	}
	if tr.HitPolicy != "first" {
		t.Errorf("hitPolicy = %q, attendu first", tr.HitPolicy)
	}
	out, ok := tr.Output.(map[string]any)
	if !ok || out["reason"] != "score insuffisant" || out["eligible"] != false {
		t.Errorf("sortie = %+v, attendu eligible=false reason=\"score insuffisant\"", tr.Output)
	}
	c := cell(tr, "credit_score")
	if c == nil {
		t.Fatalf("cellule justifiante credit_score absente, cells=%+v", tr.Cells)
	}
	if c.Value != "500" {
		t.Errorf("valeur credit_score = %q, attendu 500", c.Value)
	}
	if c.Src == "" {
		t.Errorf("la cellule justifiante doit porter son texte source (Src)")
	}
}

// Cas approuvé : la règle gagnante diffère.
func TestExplainCreditApproved(t *testing.T) {
	cm := loadCredit(t)
	tr, err := explain.Explain(cm, "eligibility", map[string]any{
		"credit_score": 720, "annual_income": 60000, "monthly_debt": 1500, "age": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	out, _ := tr.Output.(map[string]any)
	if out["eligible"] != true || out["reason"] != "approuvé" {
		t.Errorf("sortie = %+v, attendu eligible=true reason=\"approuvé\"", tr.Output)
	}
}

// Régression (revue adverse) : sous FIRST, Trace doit COURT-CIRCUITER comme Eval. La règle 1
// (géométrique) matche pour x=0 ; la règle 2 a une cellule Op=Prog qui erre (division par zéro).
// Eval renvoie "zero" sans toucher la règle 2 → Trace ne doit PAS errer non plus.
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
		t.Fatalf("Trace a erré là où Eval réussit (divergence FIRST): %v", err)
	}
	if tr.RuleIndex != 1 || tr.Output != "zero" {
		t.Errorf("attendu règle #1 -> \"zero\", obtenu #%d -> %v", tr.RuleIndex, tr.Output)
	}
}

// Une décision literal-expression (dti) est marquée 'non géométrique' sans mentir, avec sa source.
func TestExplainLiteralExprIsHonest(t *testing.T) {
	cm := loadCredit(t)
	tr, err := explain.Explain(cm, "dti", map[string]any{
		"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40,
	})
	if err != nil {
		t.Fatal(err)
	}
	if tr.Kind != "literal-expr" {
		t.Errorf("kind = %q, attendu literal-expr", tr.Kind)
	}
	if !tr.NotGeometric {
		t.Errorf("une expression doit être marquée non géométrique (honnêteté)")
	}
	if tr.ExprSrc == "" {
		t.Errorf("la source de l'expression (ExprSrc) doit être renseignée")
	}
}
