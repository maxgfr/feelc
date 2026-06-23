package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
)

// Progressive (marginal) brackets lower to arithmetic bytecode; verify the exact tax per tranche.
func TestBracketMarginalTax(t *testing.T) {
	src := `model "income_tax" {}
input taxable : number >= 0
decision tax : number {
  bracket: taxable
  [0..11294)      => 0%
  [11294..28797)  => 11%
  [28797..82341)  => 30%
  >= 82341        => 41%
}`
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		taxable int
		want    string // exact decimal
	}{
		{5000, "0"},          // below the first threshold
		{11294, "0"},         // exactly at the boundary: still 0
		{30000, "2286.23"},   // 17503*0.11 + 1203*0.30
		{100000, "25228.72"}, // ... + 53544*0.30 + 17659*0.41
	}
	for _, c := range cases {
		out, err := engine.Eval(cm, "tax", map[string]any{"taxable": c.taxable})
		if err != nil {
			t.Fatalf("taxable=%d: %v", c.taxable, err)
		}
		b, _ := json.Marshal(canon(out))
		if string(b) != `"`+c.want+`"` {
			t.Errorf("tax(%d) = %s, want %q", c.taxable, b, c.want)
		}
	}
}

func TestBracketRejectsDefault(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input x : number >= 0
decision t : number {
  bracket: x
  [0..10) => 0%
  default => 5%
}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := compiler.Compile(m); err == nil {
		t.Fatal("a bracket with `default` must be rejected by the compiler")
	}
}
