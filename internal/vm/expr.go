package vm

import (
	"errors"
	"fmt"
	"strings"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// errNAComparison: reaching a non-applicable value in an expression comparison is a loud error
// (ADR 0013 honest-degradation). NumCompare/ValueEq keep treating NA like null for the GEOMETRIC
// matching path (match.go); this guard applies only at the expression level (here).
var errNAComparison = errors.New("non-applicable value reached in a comparison")

// vmFault is a typed panic raised by the bytecode interpreter on an internal invariant violation
// (e.g. stack underflow from a corrupt program). evalExpr recovers it into a clean error; any other
// panic is re-raised so genuine bugs surface instead of being masked.
type vmFault struct{ msg string }

// evalExpr executes a bytecode program. input != nil for an Op=Prog cell
// (value of the `?` column). OpLoadVar goes through the demand-driven resolver.
func (e *evaluator) evalExpr(p *ir.ExprProgram, input *ir.Value) (result ir.Value, err error) {
	// Defensive: the compiler never emits a program that pops an empty stack, but a malformed/corrupt
	// program (e.g. a tampered .ir.bin loaded from disk) could. pop() raises a typed vmFault on
	// underflow, which we convert to a clean engine error here — any OTHER panic is re-raised so real
	// bugs are never masked.
	defer func() {
		if r := recover(); r != nil {
			if f, ok := r.(vmFault); ok {
				result, err = ir.Value{}, fmt.Errorf("malformed expression program: %s", f.msg)
				return
			}
			panic(r)
		}
	}()
	stack := make([]ir.Value, 0, p.MaxStack+1)
	push := func(v ir.Value) { stack = append(stack, v) }
	pop := func() ir.Value {
		if len(stack) == 0 {
			panic(vmFault{"stack underflow in expression bytecode"})
		}
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
			if a.Tag == ir.TagNA || b.Tag == ir.TagNA {
				return ir.Value{}, errNAComparison
			}
			push(ir.Bool(ir.ValueEq(a, b)))
		case ir.OpNeOp:
			b, a := pop(), pop()
			if a.Tag == ir.TagNA || b.Tag == ir.TagNA {
				return ir.Value{}, errNAComparison
			}
			push(ir.Bool(!ir.ValueEq(a, b)))
		case ir.OpLtOp, ir.OpLeOp, ir.OpGtOp, ir.OpGeOp:
			b, a := pop(), pop()
			if a.Tag == ir.TagNA || b.Tag == ir.TagNA {
				return ir.Value{}, errNAComparison
			}
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
		case ir.OpFloor, ir.OpCeil, ir.OpRound, ir.OpAbs, ir.OpTrunc:
			r, err := unaryNum(in.Op, pop())
			if err != nil {
				return ir.Value{}, err
			}
			push(r)
		case ir.OpRoundN, ir.OpMod, ir.OpPow:
			b, a := pop(), pop()
			r, err := binaryNum(in.Op, a, b)
			if err != nil {
				return ir.Value{}, err
			}
			push(r)
		case ir.OpStartsWith, ir.OpEndsWith, ir.OpContains:
			b, a := pop(), pop()
			r, err := stringPred(in.Op, a, b)
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
	// Non-applicable propagation (ADR 0013): in a SUM (+/-) a non-applicable term acts as 0 (so it
	// drops out); in a PRODUCT/quotient it poisons the result to non-applicable (sum-vs-product
	// semantics).
	if a.Tag == ir.TagNA || b.Tag == ir.TagNA {
		switch op {
		case ir.OpAdd, ir.OpSub:
			if a.Tag == ir.TagNA && b.Tag == ir.TagNA {
				return ir.NA(), nil
			}
			zero := ir.Num(decimal.FromInt(0))
			if a.Tag == ir.TagNA {
				a = zero
			} else {
				b = zero
			}
		default: // OpMul, OpDivOp
			return ir.NA(), nil
		}
	}
	if a.Tag == ir.TagDate || a.Tag == ir.TagDuration || b.Tag == ir.TagDate || b.Tag == ir.TagDuration {
		return temporalArith(op, a, b)
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

// temporalArith implements day-based date/duration arithmetic (ADR 0014): date-date=duration,
// date±duration=date, duration±duration=duration. Everything else (date+date, date*x, scaling,
// floor/ceiling on a date, …) is a loud error — never a silent nonsense result.
var errTemporalOverflow = errors.New("date/duration arithmetic overflow")

func temporalArith(op ir.Opcode, a, b ir.Value) (ir.Value, error) {
	da, err := dayCount(a)
	if err != nil {
		return ir.Value{}, err
	}
	db, err := dayCount(b)
	if err != nil {
		return ir.Value{}, err
	}
	ta, tb := a.Tag, b.Tag
	mk := func(days int64, ok bool, dur bool) (ir.Value, error) {
		if !ok {
			return ir.Value{}, errTemporalOverflow
		}
		if dur {
			return ir.Duration(days), nil
		}
		return ir.Date(days), nil
	}
	switch op {
	case ir.OpAdd:
		s, ok := addCheck(da, db)
		switch {
		case ta == ir.TagDate && tb == ir.TagDuration, ta == ir.TagDuration && tb == ir.TagDate:
			return mk(s, ok, false)
		case ta == ir.TagDuration && tb == ir.TagDuration:
			return mk(s, ok, true)
		}
	case ir.OpSub:
		d, ok := subCheck(da, db)
		switch {
		case ta == ir.TagDate && tb == ir.TagDate:
			return mk(d, ok, true)
		case ta == ir.TagDate && tb == ir.TagDuration:
			return mk(d, ok, false)
		case ta == ir.TagDuration && tb == ir.TagDuration:
			return mk(d, ok, true)
		}
	}
	return ir.Value{}, fmt.Errorf("unsupported date/duration arithmetic")
}

// dayCount extracts the exact whole-day count of a date/duration Value, erroring (never silently 0)
// on a nil, non-integer, or out-of-int64-range magnitude.
func dayCount(v ir.Value) (int64, error) {
	if v.Num == nil {
		return 0, fmt.Errorf("date/duration value missing its day count")
	}
	n, err := v.Num.Int64()
	if err != nil {
		return 0, fmt.Errorf("date/duration is not a whole, in-range number of days")
	}
	return n, nil
}

// addCheck/subCheck are overflow-checked int64 add/subtract (ok=false on wrap).
func addCheck(a, b int64) (int64, bool) {
	s := a + b
	if (b > 0 && s < a) || (b < 0 && s > a) {
		return 0, false
	}
	return s, true
}

func subCheck(a, b int64) (int64, bool) {
	d := a - b
	if (b < 0 && d < a) || (b > 0 && d > a) {
		return 0, false
	}
	return d, true
}

// unaryNum applies a single-arg numeric built-in (floor/ceiling/round/abs/trunc). Frozen
// decimal context (HALF_EVEN) -> determinism preserved (ADR 0002). null propagated (three-valued).
func unaryNum(op ir.Opcode, a ir.Value) (ir.Value, error) {
	if a.Tag == ir.TagNull {
		return ir.Null(), nil
	}
	if a.Tag == ir.TagNA {
		return ir.NA(), nil // non-applicable propagates through the built-in
	}
	if a.Tag != ir.TagNumber {
		return ir.Value{}, fmt.Errorf("numeric built-in (floor/ceiling/round/abs/trunc) on a non-numeric value")
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
	case ir.OpAbs:
		_, err = decimal.Ctx.Abs(r, a.Num)
	case ir.OpTrunc:
		// truncate toward zero: floor for non-negative, ceil for negative.
		if a.Num.Sign() < 0 {
			_, err = decimal.Ctx.Ceil(r, a.Num)
		} else {
			_, err = decimal.Ctx.Floor(r, a.Num)
		}
	}
	if err != nil {
		return ir.Value{}, err
	}
	return ir.Num(r), nil
}

// binaryNum applies a deterministic two-arg numeric built-in: round(x, n) (round to n decimal
// places, HALF_EVEN) and modulo(x, y) (DMN floored modulo). null/non-applicable propagate; both
// operands must be numeric. Frozen decimal context -> determinism preserved (ADR 0002).
func binaryNum(op ir.Opcode, a, b ir.Value) (ir.Value, error) {
	if a.Tag == ir.TagNull || b.Tag == ir.TagNull {
		return ir.Null(), nil
	}
	if a.Tag == ir.TagNA || b.Tag == ir.TagNA {
		return ir.NA(), nil
	}
	if a.Tag != ir.TagNumber || b.Tag != ir.TagNumber {
		return ir.Value{}, fmt.Errorf("numeric built-in (round/modulo/power) on a non-numeric value")
	}
	r := new(apd.Decimal)
	switch op {
	case ir.OpPow:
		// power(x, n): non-negative integer exponent, EXACT via repeated multiplication on the frozen
		// Decimal128 context — bit-identical to writing x*x*...*x. Never apd.Pow (transcendental,
		// inexact even for integer exponents). null/NA already handled above.
		n, err := b.Num.Int64()
		if err != nil {
			return ir.Value{}, fmt.Errorf("power(x, n): n must be a whole number")
		}
		if n < 0 {
			return ir.Value{}, fmt.Errorf("power(x, n): exponent must be non-negative (got %d) — negative powers are not exact", n)
		}
		if n > 1000 {
			return ir.Value{}, fmt.Errorf("power(x, n): exponent out of range (got %d, max 1000)", n)
		}
		r.Set(apd.New(1, 0)) // x^0 = 1 (including 0^0 = 1 by convention)
		for i := int64(0); i < n; i++ {
			if _, err := decimal.Ctx.Mul(r, r, a.Num); err != nil {
				return ir.Value{}, err
			}
		}
		return ir.Num(r), nil
	case ir.OpRoundN:
		n, err := b.Num.Int64()
		if err != nil {
			return ir.Value{}, fmt.Errorf("round(x, n): n must be a whole number")
		}
		if n < -1000 || n > 1000 {
			return ir.Value{}, fmt.Errorf("round(x, n): n out of range")
		}
		if _, err := decimal.Ctx.Quantize(r, a.Num, int32(-n)); err != nil {
			return ir.Value{}, fmt.Errorf("round(x, %d): %w", n, err)
		}
		return ir.Num(r), nil
	case ir.OpMod:
		if b.Num.Sign() == 0 {
			return ir.Value{}, fmt.Errorf("modulo by zero")
		}
		// DMN modulo: x - y*floor(x/y) (the result follows the divisor's sign).
		q := new(apd.Decimal)
		if _, err := decimal.Ctx.Quo(q, a.Num, b.Num); err != nil {
			return ir.Value{}, err
		}
		if _, err := decimal.Ctx.Floor(q, q); err != nil {
			return ir.Value{}, err
		}
		prod := new(apd.Decimal)
		if _, err := decimal.Ctx.Mul(prod, b.Num, q); err != nil {
			return ir.Value{}, err
		}
		if _, err := decimal.Ctx.Sub(r, a.Num, prod); err != nil {
			return ir.Value{}, err
		}
		return ir.Num(r), nil
	}
	return ir.Value{}, fmt.Errorf("unsupported two-arg built-in opcode %d", op)
}

// stringPred applies a total (string, string) -> boolean predicate (starts_with/ends_with/contains).
// null/non-applicable propagate; both operands must be strings (no coercion).
func stringPred(op ir.Opcode, a, b ir.Value) (ir.Value, error) {
	if a.Tag == ir.TagNull || b.Tag == ir.TagNull {
		return ir.Null(), nil
	}
	if a.Tag == ir.TagNA || b.Tag == ir.TagNA {
		return ir.NA(), nil
	}
	if a.Tag != ir.TagString || b.Tag != ir.TagString {
		return ir.Value{}, fmt.Errorf("string predicate (starts_with/ends_with/contains) requires string operands")
	}
	switch op {
	case ir.OpStartsWith:
		return ir.Bool(strings.HasPrefix(a.Str, b.Str)), nil
	case ir.OpEndsWith:
		return ir.Bool(strings.HasSuffix(a.Str, b.Str)), nil
	case ir.OpContains:
		return ir.Bool(strings.Contains(a.Str, b.Str)), nil
	}
	return ir.Value{}, fmt.Errorf("unknown string predicate opcode %d", op)
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
