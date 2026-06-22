package dmnxml_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/dmnxml"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/model"
)

func condSrcs(r model.Rule) []string {
	out := make([]string, len(r.Conds))
	for i, c := range r.Conds {
		out[i] = c.Src
	}
	return out
}
func outSrcs(cells []model.Cell) []string {
	out := make([]string, len(cells))
	for i, c := range cells {
		out[i] = c.Src
	}
	return out
}
func eqStrs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Round-trip: Parse -> Export(DMN) -> Import -> Parse preserves the structure (names, rules,
// Src of cells). We avoid domains/default (documented losses) for a clean round-trip.
func TestExportImportRoundTrip(t *testing.T) {
	src := `model "rt" {}
input score : number
input cat : string
type Out = context { ok: boolean, label: string }
decision band : Out {
  needs: score
  hit: first
  >= 700 => true | "hi"
  < 700 => false | "lo"
}
decision total : number {
  needs: cat
  hit: collect sum
  "a" => 10
  "b" => 20
}
decision dti : number = score / 12`

	a, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	xml, _, err := dmnxml.Export(a)
	if err != nil {
		t.Fatal(err)
	}
	rules2, _, err := dmnxml.Import(xml)
	if err != nil {
		t.Fatalf("Import of exported XML: %v\n%s", err, xml)
	}
	b, err := dsl.Parse(rules2)
	if err != nil {
		t.Fatalf("re-parse: %v\n%s", err, rules2)
	}

	if a.Name != b.Name {
		t.Errorf("name %q != %q", a.Name, b.Name)
	}
	if len(a.Inputs) != len(b.Inputs) {
		t.Errorf("inputs %d != %d", len(a.Inputs), len(b.Inputs))
	}
	if len(a.Decisions) != len(b.Decisions) {
		t.Fatalf("decisions %d != %d", len(a.Decisions), len(b.Decisions))
	}
	for i := range a.Decisions {
		da, db := a.Decisions[i], b.Decisions[i]
		if da.Name != db.Name {
			t.Errorf("decision %d: name %q != %q", i, da.Name, db.Name)
		}
		if (da.Expr == nil) != (db.Expr == nil) {
			t.Errorf("decision %q: literal-expr inconsistent", da.Name)
			continue
		}
		if da.Expr != nil {
			if da.Expr.Src != db.Expr.Src {
				t.Errorf("decision %q: expr %q != %q", da.Name, da.Expr.Src, db.Expr.Src)
			}
			continue
		}
		if len(da.Rules) != len(db.Rules) {
			t.Errorf("decision %q: rules %d != %d", da.Name, len(da.Rules), len(db.Rules))
			continue
		}
		for k := range da.Rules {
			if !eqStrs(condSrcs(da.Rules[k]), condSrcs(db.Rules[k])) {
				t.Errorf("decision %q rule %d: conditions %v != %v", da.Name, k, condSrcs(da.Rules[k]), condSrcs(db.Rules[k]))
			}
			if !eqStrs(outSrcs(da.Rules[k].Outputs), outSrcs(db.Rules[k].Outputs)) {
				t.Errorf("decision %q rule %d: outputs %v != %v", da.Name, k, outSrcs(da.Rules[k].Outputs), outSrcs(db.Rules[k].Outputs))
			}
		}
	}
}

// Non-silent warnings: input domain not exported (credit has one).
func TestExportWarnsOnDroppedDomain(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "credit", "credit.rules"))
	if err != nil {
		t.Fatal(err)
	}
	m, err := dsl.Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	_, warns, err := dmnxml.Export(m)
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(warns, "\n")
	if !strings.Contains(joined, "domain") {
		t.Errorf("expected a warning about a non-exported domain, got: %v", warns)
	}
}

// Regression (adversarial review): a `default` row must be REPORTED (DMN has no default).
func TestExportWarnsOnDefaultRow(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input a : number
decision d : number {
  needs: a
  hit: first
  >= 0 => 1
  default => 0
}`)
	if err != nil {
		t.Fatal(err)
	}
	_, warns, _ := dmnxml.Export(m)
	if !strings.Contains(strings.Join(warns, "\n"), "default") {
		t.Errorf("expected a warning about the `default` row, got: %v", warns)
	}
}

// Regression (adversarial review): attribute values are XML-escaped (not via Go %q).
func TestExportEscapesAttributes(t *testing.T) {
	m, err := dsl.Parse("model \"a&b\" {}\ninput x : number\n")
	if err != nil {
		t.Fatal(err)
	}
	xml, _, err := dmnxml.Export(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(xml), `name="a&amp;b"`) {
		t.Errorf("the `&` in the model name must be escaped as `&amp;`:\n%s", xml)
	}
	if strings.Contains(string(xml), `name="a&b"`) {
		t.Errorf("unescaped attribute (invalid XML):\n%s", xml)
	}
}
