// Package smt traduit la couche géométrique + bytecode de feelc en SMT-LIB2 (théorie des Reals
// et Bools), pour qu'un solveur (Z3) puisse décider des propriétés (complétude, conflits) sur les
// tables à cellules non géométriques (Op=Prog), là où l'algèbre d'hyper-rectangles s'arrête.
//
// CE PAQUET EST PUR (aucune dépendance à un binaire externe) et donc UNITAIREMENT TESTABLE sans
// Z3. L'invocation du solveur et le câblage dans la vérification vivent derrière le build tag
// `smt` (internal/verify/verify_smt.go). Sous-ensemble encodable : arithmétique +-*/, comparaisons,
// and/or/not, intervalles, ensembles, négation ; colonnes number (Real) / boolean (Bool). Tout le
// reste (if/then/else compilé en sauts, floor/ceiling/round, colonnes string, références à des
// décisions) est REFUSÉ proprement (ok=false) → la vérification reste honnête (not-verifiable).
package smt

import (
	"fmt"
	"strings"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/ir"
)

// VarResolver renvoie le nom de variable SMT d'un nom feelc (input), et false s'il n'est pas
// encodable (ex: une référence à une décision plutôt qu'à un input scalaire).
type VarResolver func(name string) (string, bool)

// Literal encode une Value scalaire en littéral SMT-LIB (Real/Bool). Les négatifs deviennent
// `(- x)` (SMT-LIB n'a pas de littéral négatif). Les chaînes ne sont pas encodables.
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
		return "", false // string / null : non encodable en Real/Bool
	}
}

// Program encode un ExprProgram STRAIGHT-LINE (sans saut) en s-expression SMT-LIB. colVar est la
// variable de la colonne courante (`?`), "" si hors cellule. ok=false si un opcode est hors
// sous-ensemble (sauts/if, floor/ceiling/round, etc.).
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
			// OpJmp/OpJmpFalse (if/then/else), OpFloor/OpCeil/OpRound, OpNeg : hors sous-ensemble.
			return "", false
		}
	}
	if len(st) != 1 {
		return "", false
	}
	return st[0], true
}

// Cell encode une CellTest en contrainte booléenne SMT sur colVar (true si la cellule matche).
// ok=false si la cellule contient un littéral/opcode non encodable.
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
