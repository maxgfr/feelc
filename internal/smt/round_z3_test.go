//go:build smt

package smt_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/smt"
)

func runZ3T(t *testing.T, query string) string {
	t.Helper()
	z3, err := exec.LookPath("z3")
	if err != nil {
		t.Skip("z3 not in PATH")
	}
	cmd := exec.Command(z3, "-in")
	cmd.Stdin = strings.NewReader(query)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("z3 failed: %v (out=%q)", err, out)
	}
	return strings.TrimSpace(string(out))
}

// TestRoundSoundnessZ3 proves the round(x) encoding computes feelc's HALF_EVEN value EXACTLY: for
// each fixed x, the constraints admit ONLY k = expected (claiming k != expected is unsat), and
// k = expected is itself satisfiable. This is the soundness guard from the plan — if it ever
// fails, round must be demoted to non-encodable rather than risk a false completeness/conflict proof.
func TestRoundSoundnessZ3(t *testing.T) {
	cases := []struct {
		x, expected string
	}{
		{"2.5", "2"},   // tie -> even (down)
		{"3.5", "4"},   // tie -> even (up)
		{"0.5", "0"},   // tie -> even
		{"1.5", "2"},   // tie -> even
		{"2.4", "2"},   // below half -> down
		{"2.6", "3"},   // above half -> up
		{"-2.5", "-2"}, // tie -> even, negative
		{"-2.6", "-3"}, // below -> away from zero
		{"7", "7"},     // integer -> itself
	}
	for _, c := range cases {
		d, err := decimal.Parse(c.x)
		if err != nil {
			t.Fatalf("parse %q: %v", c.x, err)
		}
		p := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpPushConst, Arg: 0}, {Op: ir.OpRound}}, Consts: []ir.Value{ir.Num(d)}}
		aux := &smt.Aux{}
		expr, ok := smt.Program(p, "", resolveNone, aux)
		if !ok || expr != "(to_real kr0)" {
			t.Fatalf("round(%s) encode = %q,%v", c.x, expr, ok)
		}
		var head strings.Builder
		head.WriteString("(set-logic ALL)\n")
		for _, dcl := range aux.Decls {
			head.WriteString(dcl + "\n")
		}
		for _, a := range aux.Asserts {
			head.WriteString("(assert " + a + ")\n")
		}
		exp := c.expected
		if strings.HasPrefix(exp, "-") {
			exp = "(- " + exp[1:] + ")" // SMT-LIB has no negative literal
		}
		// k != expected must be UNSAT (the rounded value is uniquely determined).
		uniq := head.String() + "(assert (not (= kr0 " + exp + ")))\n(check-sat)\n"
		if got := runZ3T(t, uniq); !strings.HasPrefix(got, "unsat") {
			t.Errorf("round(%s): expected unique k=%s, but k!=%s gave %s (encoding not tight)", c.x, c.expected, c.expected, got)
		}
		// k = expected must be satisfiable.
		sat := head.String() + "(assert (= kr0 " + exp + "))\n(check-sat)\n"
		if got := runZ3T(t, sat); !strings.HasPrefix(got, "sat") {
			t.Errorf("round(%s): k=%s should be satisfiable, got %s", c.x, c.expected, got)
		}
	}
}
