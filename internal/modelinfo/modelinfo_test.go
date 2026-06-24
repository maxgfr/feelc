package modelinfo_test

import (
	"encoding/json"
	"testing"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/modelinfo"
)

func compile(t *testing.T, src string) *ir.CompiledModel {
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

const credit = `model "credit" {}
input credit_score : number in [300..850]
input member : boolean
decision dti : number = credit_score / 100
decision band : string {
  needs: credit_score
  hit: first
  < 580 => "low"
  -     => "high"
}`

// Inputs reports each input with its readable type + domain, sorted by name.
func TestInputs(t *testing.T) {
	cm := compile(t, credit)
	ins := modelinfo.Inputs(cm)
	by := map[string]modelinfo.InputInfo{}
	for _, i := range ins {
		by[i.Name] = i
	}
	if by["credit_score"].Type != "number" || by["credit_score"].Domain != "in [300..850]" {
		t.Errorf("credit_score = %+v, want number / in [300..850]", by["credit_score"])
	}
	if by["member"].Type != "boolean" {
		t.Errorf("member type = %q, want boolean", by["member"].Type)
	}
}

// Decisions reports kind ("table"|"literal-expr") and hit policy.
func TestDecisions(t *testing.T) {
	cm := compile(t, credit)
	ds := modelinfo.Decisions(cm)
	by := map[string]modelinfo.DecInfo{}
	for _, d := range ds {
		by[d.Name] = d
	}
	if by["dti"].Kind != "literal-expr" {
		t.Errorf("dti kind = %q, want literal-expr", by["dti"].Kind)
	}
	if by["band"].Kind != "table" || by["band"].HitPolicy != "first" {
		t.Errorf("band = %+v, want table/first", by["band"])
	}
}

// JSONify normalizes decimals to clean json.Number (no "1E+1").
func TestJSONify(t *testing.T) {
	got := modelinfo.JSONify(apd.New(1, 1)) // coefficient 1, exponent 1 = 10
	if got != json.Number("10") {
		t.Errorf("JSONify(10) = %#v, want json.Number(\"10\")", got)
	}
	if modelinfo.JSONify("low") != "low" {
		t.Errorf("JSONify passes non-numbers through")
	}
}

// JSONify must recurse into contexts/lists and emit trailing-zero decimals in FIXED notation, never
// scientific. This is the contract the CLI `run --json` path relies on (cmd/feelc) so its output is
// byte-identical to the HTTP service for the same model+input — regression for the "2E+3" CLI divergence.
func TestJSONify_NestedFixedNotation(t *testing.T) {
	v := map[string]any{
		"amount": apd.New(2, 3), // coefficient 2, exponent 3 = 2000 (would render "2E+3" via apd.String)
		"label":  "ok",
		"rates":  []any{apd.New(1, 2), apd.New(5, -1)}, // 100, 0.5
	}
	b, err := json.Marshal(modelinfo.JSONify(v))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	want := `{"amount":2000,"label":"ok","rates":[100,0.5]}`
	if got != want {
		t.Errorf("JSONify nested = %s, want %s", got, want)
	}
}
