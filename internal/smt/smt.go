// Package smt translates feelc's geometric layer + bytecode into SMT-LIB2 (Reals and Bools
// theories), so that a solver (Z3) can decide properties (completeness, conflicts) on tables
// with non-geometric cells (Op=Prog), where the hyper-rectangle algebra stops.
//
// THIS PACKAGE IS PURE (no dependency on an external binary) and therefore UNIT-TESTABLE without
// Z3. Solver invocation and the wiring into verification live behind the build tag
// `smt` (internal/verify/verify_smt.go). Encodable subset: arithmetic +-*/, comparisons,
// and/or/not, ranges, sets, negation; number (Real) / boolean (Bool) columns. Everything
// else (if/then/else compiled into jumps, floor/ceiling/round, string columns, references to
// decisions) is REFUSED cleanly (ok=false) → verification stays honest (not-verifiable).
package smt

import (
	"fmt"
	"strings"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/ir"
)

// VarResolver returns the SMT variable name for a feelc name (input), and false if it is not
// encodable (e.g. a reference to a decision rather than a scalar input).
type VarResolver func(name string) (string, bool)

// Literal encodes a scalar Value into an SMT-LIB literal (Real/Bool). Negatives become
// `(- x)` (SMT-LIB has no negative literal). Strings are not encodable.
func Literal(v ir.Value) (string, bool) {
	switch v.Tag {
	case ir.TagNumber:
		r := new(apd.Decimal)
		r.Reduce(v.Num)
		s := r.Text('f')
		if strings.HasPrefix(s, "-") {
			return "(- " + s[1:] + ")", true
		}
		return s, true
	case ir.TagBool:
		if v.Bool {
			return "true", true
		}
		return "false", true
	default:
		return "", false // string / null: not encodable as Real/Bool
	}
}

// Program encodes a STRAIGHT-LINE ExprProgram (no jumps) into an SMT-LIB s-expression. colVar is
// the current column variable (`?`), "" if outside a cell. ok=false if an opcode is out of the
// subset (jumps/if, floor/ceiling/round, etc.).
func Program(p *ir.ExprProgram, colVar string, resolve VarResolver) (string, bool) {
	var st []string
	push := func(s string) { st = append(st, s) }
	pop2 := func() (string, string, bool) {
		if len(st) < 2 {
			return "", "", false
		}
		b, a := st[len(st)-1], st[len(st)-2]
		st = st[:len(st)-2]
		return a, b, true
	}
	bin := func(op string) bool {
		a, b, ok := pop2()
		if !ok {
			return false
		}
		push(fmt.Sprintf("(%s %s %s)", op, a, b))
		return true
	}
	for _, in := range p.Code {
		switch in.Op {
		case ir.OpPushConst:
			lit, ok := Literal(p.Consts[in.Arg])
			if !ok {
				return "", false
			}
			push(lit)
		case ir.OpLoadVar:
			v, ok := resolve(p.Vars[in.Arg])
			if !ok {
				return "", false
			}
			push(v)
		case ir.OpLoadInput:
			if colVar == "" {
				return "", false
			}
			push(colVar)
		case ir.OpAdd:
			if !bin("+") {
				return "", false
			}
		case ir.OpSub:
			if !bin("-") {
				return "", false
			}
		case ir.OpMul:
			if !bin("*") {
				return "", false
			}
		case ir.OpDivOp:
			if !bin("/") {
				return "", false
			}
		case ir.OpEqOp:
			if !bin("=") {
				return "", false
			}
		case ir.OpNeOp:
			a, b, ok := pop2()
			if !ok {
				return "", false
			}
			push(fmt.Sprintf("(not (= %s %s))", a, b))
		case ir.OpLtOp:
			if !bin("<") {
				return "", false
			}
		case ir.OpLeOp:
			if !bin("<=") {
				return "", false
			}
		case ir.OpGtOp:
			if !bin(">") {
				return "", false
			}
		case ir.OpGeOp:
			if !bin(">=") {
				return "", false
			}
		case ir.OpAnd:
			if !bin("and") {
				return "", false
			}
		case ir.OpOr:
			if !bin("or") {
				return "", false
			}
		case ir.OpNot:
			if len(st) < 1 {
				return "", false
			}
			st[len(st)-1] = "(not " + st[len(st)-1] + ")"
		default:
			// OpJmp/OpJmpFalse (if/then/else), OpFloor/OpCeil/OpRound, OpNeg: out of subset.
			return "", false
		}
	}
	if len(st) != 1 {
		return "", false
	}
	return st[0], true
}

// Cell encodes a CellTest into an SMT boolean constraint over colVar (true if the cell matches).
// ok=false if the cell contains a non-encodable literal/opcode.
func Cell(ct ir.CellTest, colVar string, resolve VarResolver) (string, bool) {
	expr, ok := cellBase(ct, colVar, resolve)
	if !ok {
		return "", false
	}
	if ct.Negate {
		return "(not " + expr + ")", true
	}
	return expr, true
}

func cellBase(ct ir.CellTest, colVar string, resolve VarResolver) (string, bool) {
	cmp := func(op string) (string, bool) {
		lit, ok := Literal(ct.A)
		if !ok {
			return "", false
		}
		return fmt.Sprintf("(%s %s %s)", op, colVar, lit), true
	}
	switch ct.Op {
	case ir.OpAny:
		return "true", true
	case ir.OpEq:
		return cmp("=")
	case ir.OpNe:
		s, ok := cmp("=")
		if !ok {
			return "", false
		}
		return "(not " + s + ")", true
	case ir.OpLt:
		return cmp("<")
	case ir.OpLe:
		return cmp("<=")
	case ir.OpGt:
		return cmp(">")
	case ir.OpGe:
		return cmp(">=")
	case ir.OpInRange:
		lo, ok1 := Literal(ct.A)
		hi, ok2 := Literal(ct.B)
		if !ok1 || !ok2 {
			return "", false
		}
		loOp, hiOp := ">=", "<="
		if ct.AOpen {
			loOp = ">"
		}
		if ct.BOpen {
			hiOp = "<"
		}
		return fmt.Sprintf("(and (%s %s %s) (%s %s %s))", loOp, colVar, lo, hiOp, colVar, hi), true
	case ir.OpInSet:
		parts := make([]string, 0, len(ct.Sub))
		for _, sub := range ct.Sub {
			s, ok := Cell(sub, colVar, resolve)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return "(or " + strings.Join(parts, " ") + ")", true
	case ir.OpProg:
		return Program(ct.Prog, colVar, resolve)
	default:
		return "", false
	}
}
