package vm

import (
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// evalExpr executes a bytecode program. input != nil for an Op=Prog cell
// (value of the `?` column). OpLoadVar goes through the demand-driven resolver.
func (e *evaluator) evalExpr(p *ir.ExprProgram, input *ir.Value) (ir.Value, error) {
	stack := make([]ir.Value, 0, p.MaxStack+1)
	push := func(v ir.Value) { stack = append(stack, v) }
	pop := func() ir.Value {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}
	// Indexed PC (not a plain range): OpJmp/OpJmpFalse jumps (if/then/else) move pc.
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
				return ir.Value{}, fmt.Errorf("`?` used outside a table cell")
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
				return ir.Value{}, fmt.Errorf("logical operator on a non-boolean value")
			}
			if in.Op == ir.OpAnd {
				push(ir.Bool(a.Bool && b.Bool))
			} else {
				push(ir.Bool(a.Bool || b.Bool))
			}
		case ir.OpNot:
			a := pop()
			if a.Tag != ir.TagBool {
				return ir.Value{}, fmt.Errorf("`not` on a non-boolean value")
			}
			push(ir.Bool(!a.Bool))
		case ir.OpFloor, ir.OpCeil, ir.OpRound:
			r, err := unaryNum(in.Op, pop())
			if err != nil {
				return ir.Value{}, err
			}
			push(r)
		case ir.OpJmp:
			pc = int(in.Arg) - 1 // -1: the loop's pc++ will land on Arg
		case ir.OpJmpFalse:
			c := pop()
			if c.Tag != ir.TagBool {
				return ir.Value{}, fmt.Errorf("non-boolean `if` condition")
			}
			if !c.Bool {
				pc = int(in.Arg) - 1
			}
		default:
			return ir.Value{}, fmt.Errorf("unsupported opcode at execution: %d", in.Op)
		}
	}
	if len(stack) != 1 {
		return ir.Value{}, fmt.Errorf("inconsistent expression stack (size %d)", len(stack))
	}
	return stack[0], nil
}

func arith(op ir.Opcode, a, b ir.Value) (ir.Value, error) {
	if a.Tag == ir.TagNull || b.Tag == ir.TagNull {
		return ir.Null(), nil // null propagation (three-valued, cf. ADR 0003)
	}
	if a.Tag != ir.TagNumber || b.Tag != ir.TagNumber {
		return ir.Value{}, fmt.Errorf("arithmetic operation on a non-numeric value")
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
			return ir.Value{}, fmt.Errorf("division by zero")
		}
		_, err = decimal.Ctx.Quo(r, a.Num, b.Num)
	}
	if err != nil {
		return ir.Value{}, err
	}
	return ir.Num(r), nil
}

// unaryNum applies a single-arg numeric built-in (floor/ceiling/round). Frozen
// decimal context (HALF_EVEN) -> determinism preserved (ADR 0002). null propagated (three-valued).
func unaryNum(op ir.Opcode, a ir.Value) (ir.Value, error) {
	if a.Tag == ir.TagNull {
		return ir.Null(), nil
	}
	if a.Tag != ir.TagNumber {
		return ir.Value{}, fmt.Errorf("numeric built-in (floor/ceiling/round) on a non-numeric value")
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
