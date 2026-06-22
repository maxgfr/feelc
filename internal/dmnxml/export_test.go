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

// Round-trip : Parse -> Export(DMN) -> Import -> Parse conserve la structure (noms, règles,
// Src des cellules). On évite domaines/default (pertes documentées) pour un round-trip net.
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
		t.Fatalf("Import du XML exporté: %v\n%s", err, xml)
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
			t.Errorf("décision %d: nom %q != %q", i, da.Name, db.Name)
		}
		if (da.Expr == nil) != (db.Expr == nil) {
			t.Errorf("décision %q: littéral-expr incohérent", da.Name)
			continue
		}
		if da.Expr != nil {
			if da.Expr.Src != db.Expr.Src {
				t.Errorf("décision %q: expr %q != %q", da.Name, da.Expr.Src, db.Expr.Src)
			}
			continue
		}
		if len(da.Rules) != len(db.Rules) {
			t.Errorf("décision %q: règles %d != %d", da.Name, len(da.Rules), len(db.Rules))
			continue
		}
		for k := range da.Rules {
			if !eqStrs(condSrcs(da.Rules[k]), condSrcs(db.Rules[k])) {
				t.Errorf("décision %q règle %d: conditions %v != %v", da.Name, k, condSrcs(da.Rules[k]), condSrcs(db.Rules[k]))
			}
			if !eqStrs(outSrcs(da.Rules[k].Outputs), outSrcs(db.Rules[k].Outputs)) {
				t.Errorf("décision %q règle %d: sorties %v != %v", da.Name, k, outSrcs(da.Rules[k].Outputs), outSrcs(db.Rules[k].Outputs))
			}
		}
	}
}

// Avertissements NON silencieux : domaine d'entrée non exporté (crédit en a).
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
	if !strings.Contains(joined, "domaine") {
		t.Errorf("attendu un avertissement de domaine non exporté, obtenu: %v", warns)
	}
}
