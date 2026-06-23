// Package smt translates feelc's geometric layer + bytecode into SMT-LIB2 (Reals and Bools
// theories, plus Ints for built-ins), so that a solver (Z3) can decide properties (completeness,
// conflicts) on tables with non-geometric cells (Op=Prog), where the hyper-rectangle algebra stops.
//
// THIS PACKAGE IS PURE (no dependency on an external binary) and therefore UNIT-TESTABLE without
// Z3. Solver invocation and the wiring into verification live behind the build tag
// `smt` (internal/verify/verify_smt.go). Encodable subset: arithmetic +-*/ and unary negation,
// comparisons, and/or/not, ranges, sets, negation; if/then/else (via `ite`), floor/ceiling
// (via `to_int`), and round (HALF_EVEN, via a fresh integer with parity constraints — needs an
// Aux sink); number (Real) / boolean (Bool) columns. Everything else (string columns, references
// to decisions, an Aux-less round) is REFUSED cleanly (ok=false) → verification stays honest
// (not-verifiable), never a false proof.
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

// Aux accumulates auxiliary SMT declarations and side-assertions produced while encoding a
// non-algebraic built-in (round → a fresh Int constant with HALF_EVEN constraints), and hands out
// globally-unique names. The caller MUST emit Decls (with the other declarations) and Asserts (as
// top-level assertions) into the final query. A nil *Aux means "no sink available": round is then
// refused (ok=false), keeping the encoder honest rather than silently unsound.
type Aux struct {
	Decls   []string // e.g. "(declare-const kr0 Int)"
	Asserts []string // side constraints defining the fresh vars (must hold globally)
	n       int
}

func (a *Aux) fresh(prefix string) string {
	name := fmt.Sprintf("%s%d", prefix, a.n)
	a.n++
	return name
}

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

// Program encodes an ExprProgram into an SMT-LIB s-expression. colVar is the current column
// variable (`?`), "" if outside a cell. if/then/else (compiled to jumps) is reconstructed into
// `ite`; floor/ceiling map onto `to_int`; round (HALF_EVEN) needs aux≠nil. ok=false if an opcode
// is out of the subset (string column, decision dependency, Aux-less round).
func Program(p *ir.ExprProgram, colVar string, resolve VarResolver, aux *Aux) (string, bool) {
	return encodeRange(p, 0, len(p.Code), colVar, resolve, aux)
}

// encodeRange encodes the straight-line-with-nested-ifs region p.Code[lo:hi) into a single
// SMT term. Jumps come ONLY from the compiler's emitIf backpatch (cf. compiler/lower_expr.go),
// which is strictly nested and forward-only — so an `OpJmpFalse@i -> elseStart` has its matching
// `OpJmp -> end` at elseStart-1, and the region is reducible into `(ite cond then else)`.
func encodeRange(p *ir.ExprProgram, lo, hi int, colVar string, resolve VarResolver, aux *Aux) (string, bool) {
	var st []string
	push := func(s string) { st = append(st, s) }
	pop1 := func() (string, bool) {
		if len(st) < 1 {
			return "", false
		}
		x := st[len(st)-1]
		st = st[:len(st)-1]
		return x, true
	}
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
	un := func(f func(string) string) bool {
		a, ok := pop1()
		if !ok {
			return false
		}
		push(f(a))
		return true
	}
	i := lo
	for i < hi {
		in := p.Code[i]
		switch in.Op {
		case ir.OpPushConst:
			lit, ok := Literal(p.Consts[in.Arg])
			if !ok {
				return "", false
			}
			push(lit)
			i++
		case ir.OpLoadVar:
			v, ok := resolve(p.Vars[in.Arg])
			if !ok {
				return "", false
			}
			push(v)
			i++
		case ir.OpLoadInput:
			if colVar == "" {
				return "", false
			}
			push(colVar)
			i++
		case ir.OpAdd:
			if !bin("+") {
				return "", false
			}
			i++
		case ir.OpSub:
			if !bin("-") {
				return "", false
			}
			i++
		case ir.OpMul:
			if !bin("*") {
				return "", false
			}
			i++
		case ir.OpDivOp:
			if !bin("/") {
				return "", false
			}
			i++
		case ir.OpNeg:
			if !un(func(a string) string { return "(- " + a + ")" }) {
				return "", false
			}
			i++
		case ir.OpEqOp:
			if !bin("=") {
				return "", false
			}
			i++
		case ir.OpNeOp:
			a, b, ok := pop2()
			if !ok {
				return "", false
			}
			push(fmt.Sprintf("(not (= %s %s))", a, b))
			i++
		case ir.OpLtOp:
			if !bin("<") {
				return "", false
			}
			i++
		case ir.OpLeOp:
			if !bin("<=") {
				return "", false
			}
			i++
		case ir.OpGtOp:
			if !bin(">") {
				return "", false
			}
			i++
		case ir.OpGeOp:
			if !bin(">=") {
				return "", false
			}
			i++
		case ir.OpAnd:
			if !bin("and") {
				return "", false
			}
			i++
		case ir.OpOr:
			if !bin("or") {
				return "", false
			}
			i++
		case ir.OpNot:
			if !un(func(a string) string { return "(not " + a + ")" }) {
				return "", false
			}
			i++
		case ir.OpFloor:
			// floor toward -∞: SMT `to_int` IS floor; lift the Int result back to Real.
			if !un(func(a string) string { return "(to_real (to_int " + a + "))" }) {
				return "", false
			}
			i++
		case ir.OpCeil:
			// ceiling = -floor(-x).
			if !un(func(a string) string { return "(to_real (- (to_int (- " + a + "))))" }) {
				return "", false
			}
			i++
		case ir.OpRound:
			a, ok := pop1()
			if !ok {
				return "", false
			}
			s, ok := encodeRound(a, aux)
			if !ok {
				return "", false
			}
			push(s)
			i++
		case ir.OpJmpFalse:
			cond, ok := pop1()
			if !ok {
				return "", false
			}
			elseStart := int(in.Arg)
			if elseStart < i+2 || elseStart > hi {
				return "", false
			}
			jmp := p.Code[elseStart-1]
			if jmp.Op != ir.OpJmp {
				return "", false
			}
			end := int(jmp.Arg)
			if end < elseStart || end > hi {
				return "", false
			}
			thenExpr, ok := encodeRange(p, i+1, elseStart-1, colVar, resolve, aux)
			if !ok {
				return "", false
			}
			elseExpr, ok := encodeRange(p, elseStart, end, colVar, resolve, aux)
			if !ok {
				return "", false
			}
			push(fmt.Sprintf("(ite %s %s %s)", cond, thenExpr, elseExpr))
			i = end
		default:
			// Bare OpJmp (outside the if-structure) or an unknown opcode: out of subset.
			return "", false
		}
	}
	if len(st) != 1 {
		return "", false
	}
	return st[0], true
}

// encodeRound encodes round(x) (HALF_EVEN to the nearest integer) by introducing a fresh Int k
// constrained to be within 1/2 of x, with ties (|x-k| = 1/2) broken to an even k (banker's
// rounding) — an EXACT encoding (no over/under-approximation), so SMT verdicts stay sound.
// Requires aux≠nil to register the declaration + constraints; otherwise refuses (ok=false).
func encodeRound(x string, aux *Aux) (string, bool) {
	if aux == nil {
		return "", false
	}
	k := aux.fresh("kr")
	kr := "(to_real " + k + ")"
	aux.Decls = append(aux.Decls, "(declare-const "+k+" Int)")
	aux.Asserts = append(aux.Asserts,
		// k lies in the CLOSED interval [x-1/2, x+1/2] (length 1 → a unique integer, except a tie).
		"(<= (- "+x+" "+kr+") (/ 1 2))",
		"(<= (- "+kr+" "+x+") (/ 1 2))",
		// At a tie (x is a half-integer), both neighbours qualify → pick the even one.
		"(=> (= (- "+x+" "+kr+") (/ 1 2)) (= (mod "+k+" 2) 0))",
		"(=> (= (- "+kr+" "+x+") (/ 1 2)) (= (mod "+k+" 2) 0))",
	)
	return kr, true
}

// Cell encodes a CellTest into an SMT boolean constraint over colVar (true if the cell matches).
// ok=false if the cell contains a non-encodable literal/opcode.
func Cell(ct ir.CellTest, colVar string, resolve VarResolver, aux *Aux) (string, bool) {
	expr, ok := cellBase(ct, colVar, resolve, aux)
	if !ok {
		return "", false
	}
	if ct.Negate {
		return "(not " + expr + ")", true
	}
	return expr, true
}

func cellBase(ct ir.CellTest, colVar string, resolve VarResolver, aux *Aux) (string, bool) {
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
			s, ok := Cell(sub, colVar, resolve, aux)
			if !ok {
				return "", false
			}
			parts = append(parts, s)
		}
		return "(or " + strings.Join(parts, " ") + ")", true
	case ir.OpProg:
		return Program(ct.Prog, colVar, resolve, aux)
	default:
		return "", false
	}
}
