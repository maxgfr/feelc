package ir_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
)

// RequiredDecisions returns the transitive decision dependencies of a goal in
// dependency-first order (goal last) — the order in which Eval resolves them.
func TestRequiredDecisions(t *testing.T) {
	m, err := dsl.Parse(`model "t" {}
input a : number >= 0
input b : number >= 0
decision ratio : number = a / b
decision scaled : number = ratio * 2
decision band : string {
  needs: scaled
  hit: first
  < 1 => "low"
  -   => "high"
}`)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}

	got, err := cm.RequiredDecisions("band")
	if err != nil {
		t.Fatal(err)
	}
	// dependency-first, goal last: ratio (needs a,b) -> scaled (needs ratio) -> band (needs scaled)
	want := []string{"ratio", "scaled", "band"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("RequiredDecisions(band) = %v, want %v", got, want)
	}

	// a goal with no upstream decisions returns just itself.
	gotRatio, err := cm.RequiredDecisions("ratio")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(gotRatio, ",") != "ratio" {
		t.Errorf("RequiredDecisions(ratio) = %v, want [ratio]", gotRatio)
	}

	if _, err := cm.RequiredDecisions("nope"); err == nil {
		t.Error("unknown decision must error")
	}
}
