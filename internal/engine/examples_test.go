package engine_test

import (
	"os"
	"testing"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/engine"
)

func load(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func numText(t *testing.T, v any) string {
	t.Helper()
	d, ok := v.(*apd.Decimal)
	if !ok {
		t.Fatalf("expected decimal, got %T (%v)", v, v)
	}
	return d.Text('f')
}

// Insurance: COLLECT sum + DRG (premium = base + surcharge).
func TestExampleInsurance(t *testing.T) {
	src := load(t, "../../examples/insurance/insurance.rules")
	cases := []struct {
		age, claims, base int
		region            string
		wantPremium       string
	}{
		{22, 4, 1000, "urban", "1950"}, // 300+150+500 = 950
		{40, 0, 800, "rural", "800"},   // no surcharge
		{72, 0, 1000, "urban", "1350"}, // 150 + 200
	}
	for _, c := range cases {
		out, err := engine.Run(src, "premium", map[string]any{
			"age": c.age, "claims": c.claims, "base_premium": c.base, "region": c.region,
		})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		if got := numText(t, out); got != c.wantPremium {
			t.Errorf("premium(%+v) = %s, expected %s", c, got, c.wantPremium)
		}
	}
}

// Benefits: COLLECT raw (list) + boolean.
func TestExampleBenefits(t *testing.T) {
	src := load(t, "../../examples/benefits/benefits.rules")
	cases := []struct {
		income, children int
		student          bool
		wantLen          int
	}{
		{900, 2, true, 3},   // housing + family + student_grant
		{2000, 0, false, 0}, // none
		{1200, 1, false, 2}, // housing + family
	}
	for _, c := range cases {
		out, err := engine.Run(src, "aids", map[string]any{
			"income": c.income, "children": c.children, "is_student": c.student,
		})
		if err != nil {
			t.Fatalf("%+v: %v", c, err)
		}
		xs, ok := out.([]any)
		if !ok {
			t.Fatalf("aids expected list, got %T", out)
		}
		if len(xs) != c.wantLen {
			t.Errorf("aids(%+v) = %v (len %d), expected len %d", c, xs, len(xs), c.wantLen)
		}
	}
}

// Promos: COLLECT max (best discount).
func TestExamplePromo(t *testing.T) {
	src := load(t, "../../examples/promo/promo.rules")
	t.Run("max", func(t *testing.T) {
		out, err := engine.Run(src, "discount_pct", map[string]any{
			"cart_total": 120, "is_member": true, "promo_code": "BIG20",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := numText(t, out); got != "20" {
			t.Errorf("discount = %s, expected 20", got)
		}
	})
	t.Run("low threshold", func(t *testing.T) {
		out, err := engine.Run(src, "discount_pct", map[string]any{
			"cart_total": 60, "is_member": false, "promo_code": "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		if got := numText(t, out); got != "5" {
			t.Errorf("discount = %s, expected 5", got)
		}
	})
	t.Run("no discount -> null", func(t *testing.T) {
		out, err := engine.Run(src, "discount_pct", map[string]any{
			"cart_total": 30, "is_member": false, "promo_code": "none",
		})
		if err != nil {
			t.Fatal(err)
		}
		if out != nil {
			t.Errorf("discount = %v, expected null", out)
		}
	})
}
