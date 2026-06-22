package compiler

import (
	"fmt"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// lowerExpr compile un nœud FEEL en ExprProgram (bytecode plat).
// Sous-ensemble v2 : littéraux, variables (dont `?`), arithmétique +-*/, comparaisons, and/or.
// Les autres constructs (fonctions, if/then/else, for/some/every, **) échouent franchement.
func lowerExpr(node feel.Node) (*ir.ExprProgram, error) {
	l := &lowerer{prog: &ir.ExprProgram{}, varIdx: map[string]int{}}
	if err := l.emit(node); err != nil {
		return nil, err
	}
	l.prog.MaxStack = maxStack(l.prog.Code)
	return l.prog, nil
}

type lowerer struct {
	prog   *ir.ExprProgram
	varIdx map[string]int
}

func (l *lowerer) constIdx(v ir.Value) uint32 {
	idx := uint32(len(l.prog.Consts))
	l.prog.Consts = append(l.prog.Consts, v)
	return idx
}

func (l *lowerer) varIndex(name string) uint32 {
	if i, ok := l.varIdx[name]; ok {
		return uint32(i)
	}
	i := len(l.prog.Vars)
	l.prog.Vars = append(l.prog.Vars, name)
	l.varIdx[name] = i
	return uint32(i)
}

func (l *lowerer) push(op ir.Opcode, arg uint32) {
	l.prog.Code = append(l.prog.Code, ir.Instr{Op: op, Arg: arg})
}

func (l *lowerer) emit(node feel.Node) error {
	switch n := node.(type) {
	case *feel.NumberNode:
		d, err := decimal.Parse(n.Value)
		if err != nil {
			return err
		}
		l.push(ir.OpPushConst, l.constIdx(ir.Num(d)))
	case *feel.StringNode:
		l.push(ir.OpPushConst, l.constIdx(ir.Str(n.Content())))
	case *feel.BoolNode:
		l.push(ir.OpPushConst, l.constIdx(ir.Bool(n.Value)))
	case *feel.Var:
		if n.Name == "?" {
			l.push(ir.OpLoadInput, 0)
		} else {
			l.push(ir.OpLoadVar, l.varIndex(n.Name))
		}
	case *feel.Binop:
		if err := l.emit(n.Left); err != nil {
			return err
		}
		if err := l.emit(n.Right); err != nil {
			return err
		}
		op, err := binopcode(n.Op)
		if err != nil {
			return err
		}
		l.push(op, 0)
	default:
		return fmt.Errorf("expression non supportée en v2: %T", node)
	}
	return nil
}

func binopcode(op string) (ir.Opcode, error) {
	switch op {
	case "+":
		return ir.OpAdd, nil
	case "-":
		return ir.OpSub, nil
	case "*":
		return ir.OpMul, nil
	case "/":
		return ir.OpDivOp, nil
	case "=":
		return ir.OpEqOp, nil
	case "!=":
		return ir.OpNeOp, nil
	case "<":
		return ir.OpLtOp, nil
	case "<=":
		return ir.OpLeOp, nil
	case ">":
		return ir.OpGtOp, nil
	case ">=":
		return ir.OpGeOp, nil
	case "and":
		return ir.OpAnd, nil
	case "or":
		return ir.OpOr, nil
	default:
		return 0, fmt.Errorf("opérateur non supporté en v2: %q", op)
	}
}

// maxStack calcule la profondeur de pile maximale (T2 : pas de sauts).
func maxStack(code []ir.Instr) int {
	depth, max := 0, 0
	for _, in := range code {
		switch in.Op {
		case ir.OpPushConst, ir.OpLoadVar, ir.OpLoadInput:
			depth++
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDivOp,
			ir.OpEqOp, ir.OpNeOp, ir.OpLtOp, ir.OpLeOp, ir.OpGtOp, ir.OpGeOp,
			ir.OpAnd, ir.OpOr:
			depth-- // pop 2, push 1
		}
		if depth > max {
			max = depth
		}
	}
	return max
}
