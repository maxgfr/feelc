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

// The VM is the oracle: a conforming claim is supported, a false claim is contradicted.
func TestCheckOracle(t *testing.T) {
	cm := creditModel(t)
	claims := []check.Claim{
		{Desc: "good application -> approved", Decision: "eligibility",
			Input:  map[string]any{"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
			Expect: map[string]any{"eligible": true, "reason": "approved"}},
		{Desc: "low score -> rejection", Decision: "eligibility",
			Input:  map[string]any{"credit_score": 500, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
			Expect: map[string]any{"eligible": false, "reason": "insufficient score"}},
		{Desc: "dti exact", Decision: "dti",
			Input:  map[string]any{"monthly_debt": 1500, "annual_income": 60000},
			Expect: float64(0.3)},
		{Desc: "FALSE claim (wrong reason)", Decision: "eligibility",
			Input:  map[string]any{"credit_score": 700, "annual_income": 60000, "monthly_debt": 1500, "age": 40},
			Expect: map[string]any{"eligible": true, "reason": "mauvaise raison"}},
	}
	rep := check.Check(cm, claims)
	if rep.Verdicts[0].Status != check.Supported {
		t.Errorf("claim 0 = %s, expected supported (%s)", rep.Verdicts[0].Status, rep.Verdicts[0].Detail)
	}
	if rep.Verdicts[1].Status != check.Supported {
		t.Errorf("claim 1 = %s, expected supported", rep.Verdicts[1].Status)
	}
	if rep.Verdicts[2].Status != check.Supported {
		t.Errorf("claim 2 (dti) = %s, expected supported (%s)", rep.Verdicts[2].Status, rep.Verdicts[2].Detail)
	}
	if rep.Verdicts[3].Status != check.Contradicted {
		t.Errorf("claim 3 = %s, expected contradicted", rep.Verdicts[3].Status)
	}
	if rep.Blockers() != 1 {
		t.Errorf("blockers = %d, expected 1", rep.Blockers())
	}
}
