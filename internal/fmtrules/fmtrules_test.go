package fmtrules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/fmtrules"
)

func examplePaths(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, name := range []string{"credit", "benefits", "insurance", "promo"} {
		out = append(out, filepath.Join("..", "..", "examples", name, name+".rules"))
	}
	return out
}

// Idempotence : fmt(fmt(x)) == fmt(x) sur les 4 exemples + un mini. Et la sortie reparse sans erreur.
func TestIdempotentAndReparses(t *testing.T) {
	srcs := map[string]string{
		"mini": `model "m" {}
input a : number
input bb : number
decision d : number {
  needs: a, bb
  hit: first
  >= 1 | < 2 => 10
  default => 0
}`,
	}
	for _, p := range examplePaths(t) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		srcs[p] = string(b)
	}
	for name, src := range srcs {
		out1, err := fmtrules.Source(src)
		if err != nil {
			t.Fatalf("%s: fmt: %v", name, err)
		}
		out2, err := fmtrules.Source(out1)
		if err != nil {
			t.Fatalf("%s: la sortie formatée ne reparse pas: %v\n%s", name, err, out1)
		}
		if out1 != out2 {
			t.Errorf("%s: non idempotent\n--- out1 ---\n%s\n--- out2 ---\n%s", name, out1, out2)
		}
	}
}

// Round-trip structurel : Parse(Format(Parse(src))) conserve la structure (noms, types, sorties).
func TestRoundTripStructure(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "examples", "credit", "credit.rules"))
	if err != nil {
		t.Fatal(err)
	}
	m1, err := dsl.Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	out := fmtrules.Format(m1)
	m2, err := dsl.Parse(out)
	if err != nil {
		t.Fatalf("reformat ne reparse pas: %v\n%s", err, out)
	}
	if m1.Name != m2.Name {
		t.Errorf("name %q != %q", m1.Name, m2.Name)
	}
	if len(m1.Inputs) != len(m2.Inputs) {
		t.Errorf("inputs %d != %d", len(m1.Inputs), len(m2.Inputs))
	}
	if len(m1.Decisions) != len(m2.Decisions) {
		t.Fatalf("decisions %d != %d", len(m1.Decisions), len(m2.Decisions))
	}
	for i := range m1.Decisions {
		d1, d2 := m1.Decisions[i], m2.Decisions[i]
		if d1.Name != d2.Name || d1.TypeName != d2.TypeName {
			t.Errorf("décision %d: (%s:%s) != (%s:%s)", i, d1.Name, d1.TypeName, d2.Name, d2.TypeName)
		}
		if len(d1.Rules) != len(d2.Rules) {
			t.Errorf("décision %q: %d règles != %d", d1.Name, len(d1.Rules), len(d2.Rules))
		}
	}
}

// PERTES assumées : commentaires et corps du bloc `model` (rounding:) absents de la sortie.
func TestDocumentedLosses(t *testing.T) {
	src := `model "m" {
  rounding: half_even
}
input a : number   # le score
decision d : number {
  needs: a
  hit: first
  # col => out
  >= 1 => 10
  default => 0
}`
	out, err := fmtrules.Source(src)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "rounding") {
		t.Errorf("perte attendue : `rounding:` ne doit pas survivre\n%s", out)
	}
	if strings.Contains(out, "#") || strings.Contains(out, "le score") || strings.Contains(out, "col => out") {
		t.Errorf("perte attendue : les commentaires ne doivent pas survivre\n%s", out)
	}
	// La structure essentielle est préservée.
	if !strings.Contains(out, `model "m" {}`) || !strings.Contains(out, "decision d : number") {
		t.Errorf("structure non préservée\n%s", out)
	}
}
