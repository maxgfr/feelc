package engine_test

import (
	"testing"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/engine"
)

func TestRangesAndDefault(t *testing.T) {
	src := `model "m" {}
input n : number
decision band : string {
  needs: n
  hit: first
     [0..10)   => "low"
     [10..20)  => "mid"
     default   => "out"
}`
	for _, c := range []struct {
		n    int
		want string
	}{
		{5, "low"}, {0, "low"}, {10, "mid"}, {19, "mid"}, {20, "out"}, {-1, "out"},
	} {
		got, err := engine.Run(src, "band", map[string]any{"n": c.n})
		if err != nil {
			t.Fatalf("n=%d: %v", c.n, err)
		}
		if got != c.want {
			t.Errorf("band(%d) = %v, want %q", c.n, got, c.want)
		}
	}
}

func TestSetMembership(t *testing.T) {
	src := `model "m" {}
input plan : string
input lvl  : number
decision tag : string {
  needs: plan, lvl
  hit: first
     "gold","platinum" | 1,2,3 => "vip"
     -                  | -     => "std"
}`
	for _, c := range []struct {
		plan string
		lvl  int
		want string
	}{
		{"gold", 2, "vip"}, {"platinum", 1, "vip"}, {"gold", 5, "std"}, {"bronze", 1, "std"},
	} {
		got, err := engine.Run(src, "tag", map[string]any{"plan": c.plan, "lvl": c.lvl})
		if err != nil {
			t.Fatalf("(%s,%d): %v", c.plan, c.lvl, err)
		}
		if got != c.want {
			t.Errorf("tag(%q,%d) = %v, want %q", c.plan, c.lvl, got, c.want)
		}
	}
}

// Cell Op=Prog: the condition compares the `?` column to ANOTHER column (bytecode).
func TestProgCellReferencingAnotherColumn(t *testing.T) {
	src := `model "m" {}
input amount : number
input limit  : number
decision verdict : string {
  needs: amount, limit
  hit: first
     > limit | - => "over"
     -       | - => "ok"
}`
	for _, c := range []struct {
		amount, limit int
		want          string
	}{
		{100, 50, "over"}, {30, 50, "ok"}, {50, 50, "ok"},
	} {
		got, err := engine.Run(src, "verdict", map[string]any{"amount": c.amount, "limit": c.limit})
		if err != nil {
			t.Fatalf("(%d,%d): %v", c.amount, c.limit, err)
		}
		if got != c.want {
			t.Errorf("verdict(%d,%d) = %v, want %q", c.amount, c.limit, got, c.want)
		}
	}
}

// Literal-expression decision: exact arithmetic via bytecode.
func TestArithmeticExpr(t *testing.T) {
	src := `model "m" {}
input a : number
input b : number
decision total : number = a * 2 + b`
	out, err := engine.Run(src, "total", map[string]any{"a": 10, "b": 5})
	if err != nil {
		t.Fatal(err)
	}
	d, ok := out.(*apd.Decimal)
	if !ok {
		t.Fatalf("total want decimal, got %T", out)
	}
	if d.Text('f') != "25" {
		t.Errorf("total = %s, want 25", d.Text('f'))
	}
}
