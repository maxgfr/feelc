package engine_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Subtraction must work without spaces around '-', and unary minus must work on any operand.
// Regression for a scanner bug where `\-?` in the number literal greedily absorbed the minus, so
// `2-3` / `a*3-1` tokenized as two unrelated numbers (silently wrong at the raw FEEL API; a misleading
// "expression not supported" compile error end-to-end), and `-a` could not be parsed at all.
func TestMinus_BinaryAndUnary(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"a * 3-1", "29"}, // (10*3) - 1
		{"a*3-1", "29"},
		{"(a)-1", "9"},
		{"10-1", "9"},
		{"2-3", "-1"},
		{"a - 1", "9"},   // spaced subtraction still fine
		{"-a", "-10"},    // unary minus on a variable
		{"- a", "-10"},   // unary minus with a space
		{"3 * -1", "-3"}, // unary minus after an operator
		{"-(a + 1)", "-11"},
		{"0 - a", "-10"}, // the historical workaround still works
	}
	for _, c := range cases {
		src := "model \"m\" {}\n\ninput a : number\n\ndecision result : number = " + c.expr + "\n"
		got, err := engine.Run(src, "result", map[string]any{"a": 10})
		if err != nil {
			t.Errorf("%q: unexpected error: %v", c.expr, err)
			continue
		}
		if numText(t, got) != c.want {
			t.Errorf("%q = %s, want %s", c.expr, numText(t, got), c.want)
		}
	}
}

// Negative numeric LITERALS in geometric positions (cell bounds, ranges, equality) must still lex as a
// single negative number — the scanner fix is context-sensitive so it must not regress these.
func TestMinus_NegativeLiteralsInTable(t *testing.T) {
	src := `model "m" {}
input delta : number in [-10..10]
decision band : string {
  needs: delta
  hit: first
     < -5      => "very-low"
     [-5..0)   => "low"
     0         => "zero"
     > 0       => "positive"
}`
	cases := []struct {
		in   float64
		want string
	}{
		{-8, "very-low"},
		{-3, "low"},
		{0, "zero"},
		{5, "positive"},
	}
	for _, c := range cases {
		got, err := engine.Run(src, "band", map[string]any{"delta": c.in})
		if err != nil {
			t.Errorf("delta=%v: unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("delta=%v: band = %v, want %v", c.in, got, c.want)
		}
	}
}
