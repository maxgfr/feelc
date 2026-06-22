package vm

import (
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// evalExpr exécute un programme bytecode. input != nil pour une cellule Op=Prog
// (valeur de la colonne `?`). Les OpLoadVar passent par le resolver demand-driven.
func (e *evaluator) evalExpr(p *ir.ExprProgram, input *ir.Value) (ir.Value, error) {
	stack := make([]ir.Value, 0, p.MaxStack+1)
	push := func(v ir.Value) { stack = append(stack, v) }
	pop := func() ir.Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}
	// PC indexé (et non un simple range) : les sauts OpJmp/OpJmpFalse (if/then/else) déplacent pc.
	for pc := 0; pc < len(p.Code); pc++ {
		in := p.Code[pc]
		switch in.Op {
		case ir.OpPushConst:
			push(p.Consts[in.Arg])
		case ir.OpLoadVar:
			v, err := e.resolve(p.Vars[in.Arg])
			if err != nil {
				return ir.Value{}, err
			}
			push(v)
		case ir.OpLoadInput:
			if input == nil {
				return ir.Value{}, fmt.Errorf("`?` utilisé hors d'une cellule de table")
			}
			push(*input)
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDivOp:
			b, a := pop(), pop()
			r, err := arith(in.Op, a, b)
			if err != nil {
				return ir.Value{}, err
			}
			push(r)
		case ir.OpEqOp:
			b, a := pop(), pop()
			push(ir.Bool(ir.ValueEq(a, b)))
		case ir.OpNeOp:
			b, a := pop(), pop()
			push(ir.Bool(!ir.ValueEq(a, b)))
		case ir.OpLtOp, ir.OpLeOp, ir.OpGtOp, ir.OpGeOp:
			b, a := pop(), pop()
			ok, err := ir.NumCompare(cmpOp(in.Op), a, b)
			if err != nil {
				return ir.Value{}, err
			}
			push(ir.Bool(ok))
		case ir.OpAnd, ir.OpOr:
			b, a := pop(), pop()
			if a.Tag != ir.TagBool || b.Tag != ir.TagBool {
				return ir.Value{}, fmt.Errorf("opérateur logique sur une valeur non booléenne")
			}
			if in.Op == ir.OpAnd {
				push(ir.Bool(a.Bool && b.Bool))
			} else {
				push(ir.Bool(a.Bool || b.Bool))
			}
		case ir.OpNot:
			a := pop()
			if a.Tag != ir.TagBool {
				return ir.Value{}, fmt.Errorf("`not` sur une valeur non booléenne")
			}
			push(ir.Bool(!a.Bool))
		case ir.OpFloor, ir.OpCeil, ir.OpRound:
			r, err := unaryNum(in.Op, pop())
			if err != nil {
				return ir.Value{}, err
			}
			push(r)
		case ir.OpJmp:
			pc = int(in.Arg) - 1 // -1 : le pc++ de la boucle atterrira sur Arg
		case ir.OpJmpFalse:
			c := pop()
			if c.Tag != ir.TagBool {
				return ir.Value{}, fmt.Errorf("condition `if` non booléenne")
			}
			if !c.Bool {
				pc = int(in.Arg) - 1
			}
		default:
			return ir.Value{}, fmt.Errorf("opcode non supporté à l'exécution: %d", in.Op)
		}
	}
	if len(stack) != 1 {
		return ir.Value{}, fmt.Errorf("pile d'expression incohérente (taille %d)", len(stack))
	}
	return stack[0], nil
}

func arith(op ir.Opcode, a, b ir.Value) (ir.Value, error) {
	if a.Tag == ir.TagNull || b.Tag == ir.TagNull {
		return ir.Null(), nil // propagation de null (trivalent, cf. ADR 0003)
	}
	if a.Tag != ir.TagNumber || b.Tag != ir.TagNumber {
		return ir.Value{}, fmt.Errorf("opération arithmétique sur une valeur non numérique")
	}
	r := new(apd.Decimal)
	var err error
	switch op {
	case ir.OpAdd:
		_, err = decimal.Ctx.Add(r, a.Num, b.Num)
	case ir.OpSub:
		_, err = decimal.Ctx.Sub(r, a.Num, b.Num)
	case ir.OpMul:
		_, err = decimal.Ctx.Mul(r, a.Num, b.Num)
	case ir.OpDivOp:
		if b.Num.Sign() == 0 {
			return ir.Value{}, fmt.Errorf("division par zéro")
		}
		_, err = decimal.Ctx.Quo(r, a.Num, b.Num)
	}
	if err != nil {
		return ir.Value{}, err
	}
	return ir.Num(r), nil
}

// unaryNum applique un built-in numérique mono-arg (floor/ceiling/round). Contexte décimal
// figé (HALF_EVEN) -> déterminisme préservé (ADR 0002). null propagé (trivalent).
func unaryNum(op ir.Opcode, a ir.Value) (ir.Value, error) {
	if a.Tag == ir.TagNull {
		return ir.Null(), nil
	}
	if a.Tag != ir.TagNumber {
		return ir.Value{}, fmt.Errorf("built-in numérique (floor/ceiling/round) sur une valeur non numérique")
	}
	r := new(apd.Decimal)
	var err error
	switch op {
	case ir.OpFloor:
		_, err = decimal.Ctx.Floor(r, a.Num)
	case ir.OpCeil:
		_, err = decimal.Ctx.Ceil(r, a.Num)
	case ir.OpRound:
		_, err = decimal.Ctx.RoundToIntegralValue(r, a.Num)
	}
	if err != nil {
		return ir.Value{}, err
	}
	return ir.Num(r), nil
}

func cmpOp(op ir.Opcode) ir.Op {
	switch op {
	case ir.OpLtOp:
		return ir.OpLt
	case ir.OpLeOp:
		return ir.OpLe
	case ir.OpGtOp:
		return ir.OpGt
	default:
		return ir.OpGe
	}
}
