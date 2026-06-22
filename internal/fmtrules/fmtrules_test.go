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

// Idempotence: fmt(fmt(x)) == fmt(x) over the 4 examples + a mini one. And the output reparses without error.
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
			t.Fatalf("%s: formatted output does not reparse: %v\n%s", name, err, out1)
		}
		if out1 != out2 {
			t.Errorf("%s: non idempotent\n--- out1 ---\n%s\n--- out2 ---\n%s", name, out1, out2)
		}
	}
}

// Structural round-trip: Parse(Format(Parse(src))) preserves the structure (names, types, outputs).
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
		t.Fatalf("reformat does not reparse: %v\n%s", err, out)
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
			t.Errorf("decision %d: (%s:%s) != (%s:%s)", i, d1.Name, d1.TypeName, d2.Name, d2.TypeName)
		}
		if len(d1.Rules) != len(d2.Rules) {
			t.Errorf("decision %q: %d rules != %d", d1.Name, len(d1.Rules), len(d2.Rules))
		}
	}
}

// Accepted LOSSES: comments and the body of the `model` block (rounding:) are absent from the output.
func TestDocumentedLosses(t *testing.T) {
	src := `model "m" {
  rounding: half_even
}
input a : number   # the score
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
		t.Errorf("expected loss: `rounding:` must not survive\n%s", out)
	}
	if strings.Contains(out, "#") || strings.Contains(out, "the score") || strings.Contains(out, "col => out") {
		t.Errorf("expected loss: comments must not survive\n%s", out)
	}
	// The essential structure is preserved.
	if !strings.Contains(out, `model "m" {}`) || !strings.Contains(out, "decision d : number") {
		t.Errorf("structure not preserved\n%s", out)
	}
}
