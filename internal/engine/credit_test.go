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
		t.Fatalf("lecture credit.rules: %v", err)
	}
	return string(b)
}

// Exemple crédit complet : DRG (eligibility -> dti), expression FEEL (division), table
// avec ranges + comparaisons + default, sortie context {eligible, reason}, hit FIRST.
func TestCreditEligibility(t *testing.T) {
	src := loadCredit(t)
	cases := []struct {
		name                                  string
		score, income, debt, age              int
		wantEligible                          bool
		wantReason                            string
	}{
		{"approuvé", 700, 60000, 1500, 40, true, "approuvé"},                       // dti=0.3, score>=680
		{"sous conditions", 600, 60000, 1500, 30, true, "approuvé sous conditions"}, // score [580..680)
		{"score insuffisant", 500, 60000, 1500, 40, false, "score insuffisant"},
		{"endettement", 700, 60000, 3000, 40, false, "endettement trop élevé"}, // dti=0.6 > 0.43
		{"mineur", 700, 60000, 1500, 16, false, "mineur"},
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
				t.Fatalf("sortie attendue context, obtenu %T (%v)", out, out)
			}
			if m["eligible"] != c.wantEligible {
				t.Errorf("eligible = %v, attendu %v", m["eligible"], c.wantEligible)
			}
			if m["reason"] != c.wantReason {
				t.Errorf("reason = %q, attendu %q", m["reason"], c.wantReason)
			}
		})
	}
}

// La décision intermédiaire dti est évaluée à la demande (DRG) et exactement (décimal).
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
		t.Fatalf("dti attendu décimal, obtenu %T", out)
	}
	if d.Text('f') != "0.3" {
		t.Errorf("dti = %s, attendu 0.3 exact", d.Text('f'))
	}
}
