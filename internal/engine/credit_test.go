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
