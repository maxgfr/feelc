package compiler

import (
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/units"
)

// checkUnits runs compile-time dimensional analysis over the model and records each input's and
// decision's canonical unit in cm.Units. Literal-expression decisions are inferred from their
// bytecode; table decisions are dimensionless (their outputs are literals). A genuine dimension
// mismatch (e.g. adding EUR and EUR/month) is a compile error. Dimensionless operands are treated as
// unit-neutral so numeric constants (and the 0 in if/bracket branches) flow without false positives.
// Units are a TYPE concern only — runtime values stay plain decimals.
func checkUnits(cm *ir.CompiledModel, inputUnits map[string]units.Unit) error {
	c := &unitChecker{cm: cm, inputs: inputUnits, memo: map[string]units.Unit{}, inProgress: map[string]bool{}}
	for i := range cm.Decisions {
		u, err := c.unitOf(cm.Decisions[i].Name, cm.Decisions[i].Line)
		if err != nil {
			return err
		}
		if s := u.String(); s != "" {
			cm.Units[cm.Decisions[i].Name] = s
		}
	}
	return nil
}

type unitChecker struct {
	cm         *ir.CompiledModel
	inputs     map[string]units.Unit
	memo       map[string]units.Unit
	inProgress map[string]bool
}

func (c *unitChecker) unitOf(name string, line int) (units.Unit, error) {
	if u, ok := c.inputs[name]; ok {
		return u, nil
	}
	if u, ok := c.memo[name]; ok {
		return u, nil
	}
	d, ok := c.cm.Decision(name)
	if !ok {
		return units.Unit{}, nil // unknown name: dimensionless (existence is checked elsewhere)
	}
	if c.inProgress[name] {
		// Cycle guard: decision-on-decision cycles are only detected at RUNTIME (vm.go), not at
		// compile time, so this guard is load-bearing — break the descent on a back-edge and treat
		// it as dimensionless (a cyclic model fails later at evaluation anyway).
		return units.Unit{}, nil
	}
	c.inProgress[name] = true
	defer delete(c.inProgress, name)

	var result units.Unit
	if d.Kind == ir.KindLiteralExpr && d.Expr != nil {
		u, err := c.inferRange(d.Expr, 0, len(d.Expr.Code), d.Line)
		if err != nil {
			return nil, err
		}
		result = u
	} else {
		result = units.Unit{} // table: dimensionless
	}
	c.memo[name] = result
	return result, nil
}

// inferRange computes the unit of the straight-line-with-nested-ifs region p.Code[lo:hi) (same
// structured decode as the SMT encoder), enforcing dimensional rules with leniency toward
// dimensionless operands.
func (c *unitChecker) inferRange(p *ir.ExprProgram, lo, hi, line int) (units.Unit, error) {
	var st []units.Unit
	push := func(u units.Unit) { st = append(st, u) }
	pop1 := func() (units.Unit, bool) {
		if len(st) < 1 {
			return nil, false
		}
		x := st[len(st)-1]
		st = st[:len(st)-1]
		return x, true
	}
	pop2 := func() (units.Unit, units.Unit, bool) {
		if len(st) < 2 {
			return nil, nil, false
		}
		b, a := st[len(st)-1], st[len(st)-2]
		st = st[:len(st)-2]
		return a, b, true
	}
	bad := func() (units.Unit, error) {
		return nil, diag.New(diag.CodeUnsupported, line, "unit analysis: malformed expression")
	}
	addSub := func() error {
		a, b, ok := pop2()
		if !ok {
			return diag.New(diag.CodeUnsupported, line, "unit analysis: malformed expression")
		}
		u, err := c.combineAddSub(a, b, line)
		if err != nil {
			return err
		}
		push(u)
		return nil
	}
	i := lo
	for i < hi {
		in := p.Code[i]
		switch in.Op {
		case ir.OpPushConst:
			push(units.Unit{}) // literals are dimensionless (unit-neutral)
			i++
		case ir.OpLoadVar:
			u, err := c.unitOf(p.Vars[in.Arg], line)
			if err != nil {
				return nil, err
			}
			push(u)
			i++
		case ir.OpLoadInput:
			push(units.Unit{}) // column value inside a cell — units not tracked here
			i++
		case ir.OpAdd, ir.OpSub:
			if err := addSub(); err != nil {
				return nil, err
			}
			i++
		case ir.OpMul:
			a, b, ok := pop2()
			if !ok {
				return bad()
			}
			push(a.Mul(b))
			i++
		case ir.OpDivOp:
			a, b, ok := pop2()
			if !ok {
				return bad()
			}
			push(a.Div(b))
			i++
		case ir.OpNeg, ir.OpFloor, ir.OpCeil, ir.OpRound, ir.OpAbs, ir.OpTrunc:
			// unary, unit-preserving
			if len(st) < 1 {
				return bad()
			}
			i++
		case ir.OpRoundN:
			// round(x, n): result has x's unit (n is a dimensionless count of places).
			a, _, ok := pop2()
			if !ok {
				return bad()
			}
			push(a)
			i++
		case ir.OpMod:
			// modulo(x, y): operands share a dimension; the result keeps it (lenient on dimensionless).
			a, b, ok := pop2()
			if !ok {
				return bad()
			}
			u, err := c.combineAddSub(a, b, line)
			if err != nil {
				return nil, err
			}
			push(u)
			i++
		case ir.OpEqOp, ir.OpNeOp, ir.OpLtOp, ir.OpLeOp, ir.OpGtOp, ir.OpGeOp:
			a, b, ok := pop2()
			if !ok {
				return bad()
			}
			if !a.IsZero() && !b.IsZero() && !a.Equal(b) {
				return nil, c.mismatch(a, b, line, "compare")
			}
			push(units.Unit{}) // boolean result
			i++
		case ir.OpAnd, ir.OpOr:
			if _, _, ok := pop2(); !ok {
				return bad()
			}
			push(units.Unit{})
			i++
		case ir.OpNot:
			if _, ok := pop1(); !ok {
				return bad()
			}
			push(units.Unit{})
			i++
		case ir.OpJmpFalse:
			cond, ok := pop1()
			_ = cond
			if !ok {
				return bad()
			}
			elseStart := int(in.Arg)
			if elseStart < i+2 || elseStart > hi || p.Code[elseStart-1].Op != ir.OpJmp {
				return bad()
			}
			end := int(p.Code[elseStart-1].Arg)
			if end < elseStart || end > hi {
				return bad()
			}
			thenU, err := c.inferRange(p, i+1, elseStart-1, line)
			if err != nil {
				return nil, err
			}
			elseU, err := c.inferRange(p, elseStart, end, line)
			if err != nil {
				return nil, err
			}
			u, err := c.combineAddSub(thenU, elseU, line) // then/else must share a dimension (lenient on 0)
			if err != nil {
				return nil, c.mismatch(thenU, elseU, line, "if/then/else branches")
			}
			push(u)
			i = end
		default:
			return bad()
		}
	}
	if len(st) != 1 {
		return bad()
	}
	return st[0], nil
}

// combineAddSub returns the unit of a+b / a-b: dimensionless operands are absorbed (numeric
// constants are unit-neutral); otherwise both must share a dimension.
func (c *unitChecker) combineAddSub(a, b units.Unit, line int) (units.Unit, error) {
	switch {
	case a.IsZero():
		return b, nil
	case b.IsZero():
		return a, nil
	case a.Equal(b):
		return a, nil
	default:
		return nil, c.mismatch(a, b, line, "add/subtract")
	}
}

func (c *unitChecker) mismatch(a, b units.Unit, line int, where string) error {
	return diag.Newf(diag.CodeUnsupported, line, "unit mismatch (%s): %q vs %q", where, unitStr(a), unitStr(b)).
		WithSuggestion("operands must share the same physical unit")
}

func unitStr(u units.Unit) string {
	if s := u.String(); s != "" {
		return s
	}
	return "(dimensionless)"
}
