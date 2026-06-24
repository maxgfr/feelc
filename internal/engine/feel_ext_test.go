package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/verify"
)

// not(<test>) stays GEOMETRIC: the verifier analyzes it (no Op=Prog degradation).
// A table `"urban"` + `not("urban")` covers the whole domain -> no blocking gap.
func TestNotCellStaysVerifiable(t *testing.T) {
	src := `model "m" {}
input region : string in {"urban", "suburban", "rural"}
decision z : string {
  needs: region
  hit: first
  "urban"      => "u"
  not("urban") => "other"
}`
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	rep := verify.Verify(cm)
	for _, f := range rep.Findings {
		if f.Kind == verify.KindNotVerifiable {
			t.Errorf("not() must NOT degrade to not-verifiable: %+v", f)
		}
		if f.Severity == verify.SevError {
			t.Errorf("complete table via not(): no blocker expected, got %+v", f)
		}
	}
}

// jn passes an EXACT input number (json.Number) — no lossy float64.
func jn(s string) any { return json.Number(s) }

// if/then/else in a literal-expression decision (backpatch OpJmpFalse/OpJmp).
func TestIfThenElse(t *testing.T) {
	src := `model "m" {}
input score : number
decision tier : number = if score >= 700 then 1 else 0`
	if got, err := engine.Run(src, "tier", map[string]any{"score": jn("750")}); err != nil || numText(t, got) != "1" {
		t.Fatalf("if(score=750) = %v (err %v), expected 1", got, err)
	}
	if got, err := engine.Run(src, "tier", map[string]any{"score": jn("600")}); err != nil || numText(t, got) != "0" {
		t.Fatalf("if(score=600) = %v (err %v), expected 0", got, err)
	}
}

// Mono-arg built-ins floor / ceiling / round (round = HALF_EVEN, deterministic).
func TestMonoArgBuiltins(t *testing.T) {
	src := `model "m" {}
input x : number
decision fl : number = floor(x)
decision ce : number = ceiling(x)
decision rd : number = round(x)`
	cases := []struct {
		dec  string
		x    string
		want string
	}{
		{"fl", "2.7", "2"}, {"fl", "-2.1", "-3"},
		{"ce", "2.1", "3"}, {"ce", "-2.7", "-2"},
		{"rd", "2.5", "2"}, {"rd", "3.5", "4"}, {"rd", "2.4", "2"}, {"rd", "2.6", "3"},
	}
	for _, c := range cases {
		got, err := engine.Run(src, c.dec, map[string]any{"x": jn(c.x)})
		if err != nil {
			t.Fatalf("%s(%s): %v", c.dec, c.x, err)
		}
		if numText(t, got) != c.want {
			t.Errorf("%s(%s) = %s, expected %s", c.dec, c.x, numText(t, got), c.want)
		}
	}
}

// not(<test>) in a cell: geometric negation (value equivalence, interval, comparison),
// + multi-test not(a, b) = outside the set.
func TestNotCellNegation(t *testing.T) {
	src := `model "m" {}
input region : string in {"urban", "suburban", "rural"}
input n : number in [0..100]
decision zone : string {
  needs: region
  hit: first
  not("urban") => "other"
  -            => "urban_zone"
}
decision band : string {
  needs: n
  hit: first
  not([0..10]) => "out"
  -            => "in"
}
decision cmp : string {
  needs: n
  hit: first
  not(? < 5) => "ge5"
  -          => "lt5"
}
decision set : string {
  needs: n
  hit: first
  not(1, 2, 3) => "other"
  -            => "small"
}`
	check := func(dec string, in map[string]any, want string) {
		got, err := engine.Run(src, dec, in)
		if err != nil {
			t.Fatalf("%s%v: %v", dec, in, err)
		}
		if got != want {
			t.Errorf("%s%v = %v, expected %q", dec, in, got, want)
		}
	}
	check("zone", map[string]any{"region": "rural"}, "other")
	check("zone", map[string]any{"region": "urban"}, "urban_zone")
	check("band", map[string]any{"n": jn("50")}, "out")
	check("band", map[string]any{"n": jn("5")}, "in")
	check("cmp", map[string]any{"n": jn("10")}, "ge5")
	check("cmp", map[string]any{"n": jn("3")}, "lt5")
	check("set", map[string]any{"n": jn("7")}, "other")
	check("set", map[string]any{"n": jn("2")}, "small")
}

// Deterministic multi-arg / extra built-ins (ADR 0020): abs, trunc, round(x,n), modulo(x,y).
func TestArithBuiltins(t *testing.T) {
	src := `model "m" {}
input x : number
input y : number
input k : number
decision ab : number = abs(x)
decision tr : number = trunc(x)
decision r2 : number = round(x, 2)
decision rk : number = round(x, k)
decision md : number = modulo(x, y)`
	cases := []struct {
		dec, x, y, k, want string
	}{
		// abs
		{"ab", "-2.5", "0", "0", "2.5"}, {"ab", "2.5", "0", "0", "2.5"}, {"ab", "0", "0", "0", "0"},
		// trunc (toward zero)
		{"tr", "2.7", "0", "0", "2"}, {"tr", "-2.7", "0", "0", "-2"}, {"tr", "2", "0", "0", "2"},
		// round to N decimals (HALF_EVEN)
		{"r2", "3.14159", "0", "0", "3.14"}, {"r2", "2.71828", "0", "0", "2.72"},
		{"r2", "1.235", "0", "0", "1.24"}, {"r2", "1.245", "0", "0", "1.24"},
		// round with runtime n
		{"rk", "3.14159", "0", "3", "3.142"}, {"rk", "3.14159", "0", "0", "3"},
		// modulo (floored, DMN semantics: result follows the divisor's sign)
		{"md", "10", "3", "0", "1"}, {"md", "10", "-3", "0", "-2"},
		{"md", "-10", "3", "0", "2"}, {"md", "7.5", "2", "0", "1.5"},
	}
	for _, c := range cases {
		got, err := engine.Run(src, c.dec, map[string]any{"x": jn(c.x), "y": jn(c.y), "k": jn(c.k)})
		if err != nil {
			t.Fatalf("%s(x=%s,y=%s,k=%s): %v", c.dec, c.x, c.y, c.k, err)
		}
		if numText(t, got) != c.want {
			t.Errorf("%s(x=%s,y=%s,k=%s) = %s, expected %s", c.dec, c.x, c.y, c.k, numText(t, got), c.want)
		}
	}
}

// Error cases for the new built-ins: modulo by zero, round to a non-integer number of places.
func TestArithBuiltinErrors(t *testing.T) {
	cases := []struct{ name, src, dec string; in map[string]any; wantSub string }{
		{"modulo by zero", "model \"m\" {}\ninput x : number\ninput y : number\ndecision d : number = modulo(x, y)", "d",
			map[string]any{"x": jn("10"), "y": jn("0")}, "zero"},
		{"round non-integer n", "model \"m\" {}\ninput x : number\ninput k : number\ndecision d : number = round(x, k)", "d",
			map[string]any{"x": jn("3.14"), "k": jn("1.5")}, "whole"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := engine.Run(c.src, c.dec, c.in)
			if err == nil {
				t.Fatalf("expected error containing %q", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, expected to contain %q", err.Error(), c.wantSub)
			}
		})
	}
}

// HARD failures: multi-arg built-in, and `?` outside a cell (literal-expr, direct or via BKM).
func TestFeelExtHardFailures(t *testing.T) {
	cases := []struct {
		name, src, wantSub string
	}{
		{"floor multi-arg (mono-arg builtin, 2 args)",
			"model \"m\" {}\ninput x : number\ndecision d : number = floor(x, 1)", "argument"},
		{"? in direct literal-expr",
			"model \"m\" {}\ninput n : number\ndecision d : number = ? + n", "cell"},
		{"? via BKM argument in literal-expr",
			"model \"m\" {}\ninput n : number\nbkm f(y:number):number = y * 2\ndecision d : number = f(?) + n", "cell"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := engine.Run(c.src, "d", map[string]any{"n": jn("1"), "x": jn("1")})
			if err == nil {
				t.Fatalf("expected compilation error (%q)", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, expected to contain %q", err.Error(), c.wantSub)
			}
		})
	}
}
