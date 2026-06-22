package check_test

import (
	"os"
	"testing"

	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
)

func creditModel(t *testing.T) *ir.CompiledModel {
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

// La VM est l'oracle : un claim conforme est supporté, un claim faux est contredit.
func TestCheckOracle(t *testing.T) {
	cm := creditModel(t)
	claims := []check.Claim{
		{Desc: "bon dossier -> approuvé", Decision: "eligibility",
			Input:  map[string]any{"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
			Expect: map[string]any{"eligible": true, "reason": "approuvé"}},
		{Desc: "score faible -> refus", Decision: "eligibility",
			Input:  map[string]any{"credit_score": 500, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
			Expect: map[string]any{"eligible": false, "reason": "score insuffisant"}},
		{Desc: "dti exact", Decision: "dti",
			Input:  map[string]any{"monthly_debt": 1500, "annual_income": 60000},
			Expect: float64(0.3)},
		{Desc: "claim FAUX (raison erronée)", Decision: "eligibility",
			Input:  map[string]any{"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
			Expect: map[string]any{"eligible": true, "reason": "mauvaise raison"}},
	}
	rep := check.Check(cm, claims)
	if rep.Verdicts[0].Status != check.Supported {
		t.Errorf("claim 0 = %s, attendu supported (%s)", rep.Verdicts[0].Status, rep.Verdicts[0].Detail)
	}
	if rep.Verdicts[1].Status != check.Supported {
		t.Errorf("claim 1 = %s, attendu supported", rep.Verdicts[1].Status)
	}
	if rep.Verdicts[2].Status != check.Supported {
		t.Errorf("claim 2 (dti) = %s, attendu supported (%s)", rep.Verdicts[2].Status, rep.Verdicts[2].Detail)
	}
	if rep.Verdicts[3].Status != check.Contradicted {
		t.Errorf("claim 3 = %s, attendu contradicted", rep.Verdicts[3].Status)
	}
	if rep.Blockers() != 1 {
		t.Errorf("blockers = %d, attendu 1", rep.Blockers())
	}
}
