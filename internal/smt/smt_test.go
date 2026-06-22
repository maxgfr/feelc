package smt_test

import (
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
		t.Errorf("Literal(-3) = %q,%v (négatif doit devenir (- 3))", s, ok)
	}
	if s, ok := smt.Literal(ir.Bool(true)); !ok || s != "true" {
		t.Errorf("Literal(true) = %q,%v", s, ok)
	}
	if _, ok := smt.Literal(ir.Str("x")); ok {
		t.Errorf("Literal(string) doit être non encodable")
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
		got, ok := smt.Cell(c.ct, "c", resolveNone)
		if !ok || got != c.want {
			t.Errorf("Cell(%+v) = %q,%v, attendu %q", c.ct, got, ok, c.want)
		}
	}
	// Littéral string -> non encodable.
	if _, ok := smt.Cell(ir.CellTest{Op: ir.OpEq, A: ir.Str("urban")}, "c", resolveNone); ok {
		t.Errorf("Cell avec littéral string doit être non encodable")
	}
}

func TestProgramStraightLine(t *testing.T) {
	// ? < 5  ->  (< c 5)
	p := &ir.ExprProgram{
		Code:   []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpPushConst, Arg: 0}, {Op: ir.OpLtOp}},
		Consts: []ir.Value{num(5)},
	}
	got, ok := smt.Program(p, "c", resolveNone)
	if !ok || got != "(< c 5)" {
		t.Errorf("Program(? < 5) = %q,%v", got, ok)
	}
	// Saut (if/then/else) -> hors sous-ensemble.
	pj := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpJmpFalse, Arg: 3}}}
	if _, ok := smt.Program(pj, "c", resolveNone); ok {
		t.Errorf("Program avec saut doit être non encodable (ok=false)")
	}
	// floor/ceiling/round -> hors sous-ensemble.
	pf := &ir.ExprProgram{Code: []ir.Instr{{Op: ir.OpLoadInput}, {Op: ir.OpFloor}}}
	if _, ok := smt.Program(pf, "c", resolveNone); ok {
		t.Errorf("Program avec floor doit être non encodable (ok=false)")
	}
}
