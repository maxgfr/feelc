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
