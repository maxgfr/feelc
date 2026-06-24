package engine_test

import (
	"os"
	"testing"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/engine"
)

func loadCredit(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../examples/credit/credit.rules")
	if err != nil {
		t.Fatalf("reading credit.rules: %v", err)
	}
	return string(b)
}

// Full credit example: DRG (eligibility -> dti), FEEL expression (division), table
// with ranges + comparisons + default, context output {eligible, reason}, hit FIRST.
func TestCreditEligibility(t *testing.T) {
	src := loadCredit(t)
	cases := []struct {
		name                     string
		score, income, debt, age int
		wantEligible             bool
		wantReason               string
	}{
		{"approved", 700, 60000, 1500, 40, true, "approved"},                        // dti=0.3, score>=680
		{"with conditions", 600, 60000, 1500, 30, true, "approved with conditions"}, // score [580..680)
		{"insufficient score", 500, 60000, 1500, 40, false, "insufficient score"},
		{"debt", 700, 60000, 3000, 40, false, "debt too high"}, // dti=0.6 > 0.43
		{"minor", 700, 60000, 1500, 16, false, "minor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := engine.Run(src, "eligibility", map[string]any{
				"credit_score":  c.score,
				"annual_income": c.income,
				"monthly_debt":  c.debt,
				"age":           c.age,
			})
			if err != nil {
				t.Fatal(err)
			}
			m, ok := out.(map[string]any)
			if !ok {
				t.Fatalf("expected context output, got %T (%v)", out, out)
			}
			if m["eligible"] != c.wantEligible {
				t.Errorf("eligible = %v, expected %v", m["eligible"], c.wantEligible)
			}
			if m["reason"] != c.wantReason {
				t.Errorf("reason = %q, expected %q", m["reason"], c.wantReason)
			}
		})
	}
}

// Regression: annual_income = 0 is within the declared domain (number >= 0), so the model must
// be TOTAL over it — never divide by zero. The GitHub-Pages playground auto-runs the example with
// each input at its domain lower bound (annual_income -> 0), which previously crashed dti with
// "division by zero" (surfaced as "run failed (422)").
func TestCreditZeroIncomeIsTotal(t *testing.T) {
	src := loadCredit(t)

	// dti must not divide by zero for a zero-income applicant.
	t.Run("dti no income no debt", func(t *testing.T) {
		out, err := engine.Run(src, "dti", map[string]any{"annual_income": 0, "monthly_debt": 0})
		if err != nil {
			t.Fatalf("dti(income=0,debt=0): %v", err)
		}
		if got := numText(t, out); got != "0" {
			t.Errorf("dti(income=0,debt=0) = %s, want 0", got)
		}
	})

	// A zero-income applicant carrying debt is unserviceable: the ratio is undefined, treated as
	// above every threshold, so a score-passing applicant is rejected via "debt too high".
	t.Run("eligibility zero income with debt -> debt too high", func(t *testing.T) {
		out, err := engine.Run(src, "eligibility", map[string]any{
			"credit_score": 700, "annual_income": 0, "monthly_debt": 1500, "age": 40,
		})
		if err != nil {
			t.Fatalf("eligibility(income=0,debt=1500): %v", err)
		}
		m := out.(map[string]any)
		if m["eligible"] != false || m["reason"] != "debt too high" {
			t.Errorf("eligibility(income=0,debt=1500) = %v, want {false, \"debt too high\"}", m)
		}
	})

	// The exact input set the playground's auto-run produces (every input at its domain lower
	// bound): credit_score=300, annual_income=0, monthly_debt=0, age=0. It must run (no crash)
	// and reject on the FIRST rule (score < 580).
	t.Run("playground default inputs -> insufficient score", func(t *testing.T) {
		out, err := engine.Run(src, "eligibility", map[string]any{
			"credit_score": 300, "annual_income": 0, "monthly_debt": 0, "age": 0,
		})
		if err != nil {
			t.Fatalf("eligibility(playground defaults): %v", err)
		}
		m := out.(map[string]any)
		if m["eligible"] != false || m["reason"] != "insufficient score" {
			t.Errorf("eligibility(playground defaults) = %v, want {false, \"insufficient score\"}", m)
		}
	})
}

// The intermediate dti decision is evaluated on demand (DRG) and exactly (decimal).
func TestCreditDTIExact(t *testing.T) {
	src := loadCredit(t)
	out, err := engine.Run(src, "dti", map[string]any{
		"monthly_debt":  1500,
		"annual_income": 60000,
	})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := out.(*apd.Decimal)
	if !ok {
		t.Fatalf("expected decimal dti, got %T", out)
	}
	if d.Text('f') != "0.3" {
		t.Errorf("dti = %s, expected exactly 0.3", d.Text('f'))
	}
}
