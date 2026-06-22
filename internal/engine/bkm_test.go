package engine_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Scalar BKM invoked in a literal-expression decision. The invocation is inlined at
// compile time (AST substitution of parameters), zero call frames at runtime.
func TestBKMScalarInLiteralExpr(t *testing.T) {
	src := `model "m" {}
input monthly_debt : number
input annual_income : number
bkm dti(debt:number, income:number):number = debt / (income / 12)
decision verdict : boolean = dti(monthly_debt, annual_income) <= 0.36`

	got, err := engine.Run(src, "verdict", map[string]any{"monthly_debt": 1500, "annual_income": 60000})
	if err != nil {
		t.Fatalf("compile/run: %v", err)
	}
	if got != true {
		t.Errorf("verdict(dti=0.3) = %v, expected true", got)
	}
	got, err = engine.Run(src, "verdict", map[string]any{"monthly_debt": 2000, "annual_income": 60000})
	if err != nil {
		t.Fatal(err)
	}
	if got != false {
		t.Errorf("verdict(dti=0.4) = %v, expected false", got)
	}
}

// BKM invoked in a table cell (Op=Prog).
func TestBKMInTableCell(t *testing.T) {
	src := `model "m" {}
input monthly_debt : number
decision flag : boolean {
  needs: monthly_debt
  hit: first
  dti(monthly_debt, 60000) <= 0.36 => true
  -                                 => false
}
bkm dti(debt:number, income:number):number = debt / (income / 12)`

	got, err := engine.Run(src, "flag", map[string]any{"monthly_debt": 1500})
	if err != nil {
		t.Fatalf("compile/run: %v", err)
	}
	if got != true {
		t.Errorf("flag(1500) = %v, expected true", got)
	}
	got, _ = engine.Run(src, "flag", map[string]any{"monthly_debt": 2000})
	if got != false {
		t.Errorf("flag(2000) = %v, expected false", got)
	}
}

// Nested invocations f(g(x)): bounded, non-cyclic recursive inlining.
func TestBKMNested(t *testing.T) {
	src := `model "m" {}
input n : number
bkm half(x:number):number = x / 2
bkm quarter(x:number):number = half(half(x))
decision q : number = quarter(n)`

	got, err := engine.Run(src, "q", map[string]any{"n": 100})
	if err != nil {
		t.Fatalf("compile/run: %v", err)
	}
	if got == nil {
		t.Fatal("nil result")
	}
	if s, ok := got.(interface{ Text(byte) string }); ok {
		if v := s.Text('f'); v != "25" {
			t.Errorf("quarter(100) = %s, expected 25", v)
		}
	} else {
		t.Fatalf("unexpected output type: %T", got)
	}
}

// HARD failures (never conform in silence): arity, unknown BKM, recursion, kwargs, `?`.
func TestBKMHardFailures(t *testing.T) {
	base := `model "m" {}
input n : number
`
	cases := []struct {
		name    string
		extra   string
		wantSub string
	}{
		{"incorrect arity",
			"bkm f(a:number, b:number):number = a + b\ndecision d : number = f(n)", "argument"},
		{"unknown BKM",
			"decision d : number = nope(n)", "unknown"},
		{"BKM recursion",
			"bkm loop(x:number):number = loop(x)\ndecision d : number = loop(n)", "recursion"},
		{"named arguments (kwargs)",
			"bkm f(a:number, b:number):number = a + b\ndecision d : number = f(a: n, b: 1)", "named"},
		{"? in a BKM body",
			"bkm bad(x:number):number = ? + x\ndecision d : number = bad(n)", "forbidden"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := engine.Run(base+c.extra, "d", map[string]any{"n": 1})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("error = %q, expected to contain %q", err.Error(), c.wantSub)
			}
		})
	}
}
