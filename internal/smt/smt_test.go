package smt_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/smt"
)

func num(i int64) ir.Value { return ir.Num(decimal.FromInt(i)) }

func TestLiteral(t *testing.T) {
	if s, ok := smt.Literal(num(5)); !ok || s != "5" {
		t.Errorf("Literal(5) = %q,%v", s, ok)
	}
	if s, ok := smt.Literal(num(-3)); !ok || s != "(- 3)" {
		t.Errorf("Literal(-3) = %q,%v (negative must become (- 3))", s, ok)
	}
	if s, ok := smt.Literal(ir.Bool(true)); !ok || s != "true" {
		t.Errorf("Literal(true) = %q,%v", s, ok)
	}
	if _, ok := smt.Literal(ir.Str("x")); ok {
		t.Errorf("Literal(string) must be non-encodable")
	}
}

func resolveNone(string) (string, bool) { return "", false }

func TestCellGeometric(t *testing.T) {
	cases := []struct {
		ct   ir.CellTest
		want string
	}{
		{ir.CellTest{Op: ir.OpAny}, "true"},
		{ir.CellTest{Op: ir.OpLt, A: num(5)}, "(< c 5)"},
		{ir.CellTest{Op: ir.OpGe, A: num(0)}, "(>= c 0)"},
		{ir.CellTest{Op: ir.OpInRange, A: num(0), B: num(10), BOpen: true}, "(and (>= c 0) (< c 10))"},
		{ir.CellTest{Op: ir.OpLt, A: num(5), Negate: true}, "(not (< c 5))"},
	}
	for _, c := range cases {
		got, ok := smt.Cell(c.ct, "c", resolveNone, nil)
		if !ok || got != c.want {
			t.Errorf("Cell(%+v) = %q,%v, expected %q", c.ct, got, ok, c.want)
		}
	}
	// String literal -> non-encodable.
	if _, ok := smt.Cell(ir.CellTest{Op: ir.OpEq, A: ir.Str("urban")}, "c", resolveNone, nil); ok {
		t.Errorf("Cell with string literal must be non-encodable")
	}
}

func TestProgramStraightLine(t *testing.T) {
	// ? < 5  ->  (< c 5)
	p := &ir.ExprProgram{
		Code:   []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpPushConst, Arg: 0}, {Op: ir.OpLtOp}},
		Consts: []ir.Value{num(5)},
	}
	got, ok := smt.Program(p, "c", resolveNone, nil)
	if !ok || got != "(< c 5)" {
		t.Errorf("Program(? < 5) = %q,%v", got, ok)
	}
	// A MALFORMED jump (no matching OpJmp / target out of range) stays non-encodable.
	pj := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpJmpFalse, Arg: 3}}}
	if _, ok := smt.Program(pj, "c", resolveNone, nil); ok {
		t.Errorf("malformed jump must be non-encodable (ok=false)")
	}
}

// if/then/else (jumps) is reconstructed into SMT `ite`. Bytecode mirrors compiler emitIf:
// <cond> JmpFalse->elseStart <then> Jmp->end <else>.
func TestProgramIfThenElse(t *testing.T) {
	// if ? > 50 then true else false
	p := &ir.ExprProgram{
		Code: []ir.Instr{
			{Op: ir.OpLoadInput},         // 0: c
			{Op: ir.OpPushConst, Arg: 0}, // 1: 50
			{Op: ir.OpGtOp},              // 2: (> c 50)
			{Op: ir.OpJmpFalse, Arg: 6},  // 3: -> elseStart=6
			{Op: ir.OpPushConst, Arg: 1}, // 4: true
			{Op: ir.OpJmp, Arg: 7},       // 5: -> end=7
			{Op: ir.OpPushConst, Arg: 2}, // 6: false  [elseStart]
		}, // end = 7 (len)
		Consts: []ir.Value{num(50), ir.Bool(true), ir.Bool(false)},
	}
	got, ok := smt.Program(p, "c", resolveNone, nil)
	want := "(ite (> c 50) true false)"
	if !ok || got != want {
		t.Errorf("Program(if) = %q,%v, want %q", got, ok, want)
	}
}

func TestProgramFloorCeil(t *testing.T) {
	floor := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpFloor}}}
	if got, ok := smt.Program(floor, "c", resolveNone, nil); !ok || got != "(to_real (to_int c))" {
		t.Errorf("Program(floor) = %q,%v", got, ok)
	}
	ceil := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpCeil}}}
	if got, ok := smt.Program(ceil, "c", resolveNone, nil); !ok || got != "(to_real (- (to_int (- c))))" {
		t.Errorf("Program(ceiling) = %q,%v", got, ok)
	}
}

// round needs an Aux sink (for the fresh Int + parity constraints); without it, it refuses.
func TestProgramRound(t *testing.T) {
	p := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpRound}}}

	if _, ok := smt.Program(p, "c", resolveNone, nil); ok {
		t.Errorf("round without an Aux sink must be non-encodable (ok=false)")
	}

	aux := &smt.Aux{}
	got, ok := smt.Program(p, "c", resolveNone, aux)
	if !ok || got != "(to_real kr0)" {
		t.Errorf("Program(round) = %q,%v, want (to_real kr0)", got, ok)
	}
	if len(aux.Decls) != 1 || !strings.Contains(aux.Decls[0], "declare-const kr0 Int") {
		t.Errorf("round must declare a fresh Int: %v", aux.Decls)
	}
	joined := strings.Join(aux.Asserts, " ")
	if !strings.Contains(joined, "(mod kr0 2)") || !strings.Contains(joined, "(/ 1 2)") {
		t.Errorf("round constraints must encode the half-even tie via parity: %v", aux.Asserts)
	}
}
