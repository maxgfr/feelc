package explain_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/ir"
)

func compileSrc(t *testing.T, src string) *ir.CompiledModel {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

// NormalizeFullJSON must render decimal trace outputs as fixed-notation JSON numbers, matching the run
// `output` field. Without it a *apd.Decimal serializes via its TextMarshaler as a quoted scientific
// string ("1E+1" for 10), which the rich trace/graph UI would display verbatim. Regression for that.
func TestNormalizeFullJSON_FixedNotation(t *testing.T) {
	src := `model "m" {}
input cart_total : number >= 0
input is_member  : boolean
decision discount_pct : number {
  needs: cart_total, is_member
  hit: collect max
  >= 50  | -    => 5
  >= 100 | -    => 10
  -      | true => 8
}`
	cm := compileSrc(t, src)
	ft, err := explain.ExplainFull(cm, "discount_pct", map[string]any{"cart_total": 120, "is_member": true})
	if err != nil {
		t.Fatal(err)
	}
	explain.NormalizeFullJSON(ft)
	b, _ := json.Marshal(ft)
	s := string(b)
	if strings.Contains(s, "1E+1") || strings.Contains(s, "1e+1") {
		t.Errorf("trace JSON contains scientific notation: %s", s)
	}
	if strings.Contains(s, `"output":"`) {
		t.Errorf("trace output serialized as a quoted string (should be a JSON number): %s", s)
	}
	if !strings.Contains(s, `"output":10`) {
		t.Errorf("expected fixed-notation `\"output\":10` in trace JSON, got: %s", s)
	}
}
