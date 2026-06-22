package ir

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/decimal"
)

// Sémantique de matching PARTAGÉE entre la VM (exécution) et le vérificateur (analyse).
// Une seule source de vérité -> exécution et preuve s'accordent par construction.

// ValueEq : égalité typée de deux valeurs (null == null).
func ValueEq(a, b Value) bool {
	if a.Tag != b.Tag {
		return false
	}
	switch a.Tag {
	case TagNumber:
		return decimal.Cmp(a.Num, b.Num) == 0
	case TagString:
		return a.Str == b.Str
	case TagBool:
		return a.Bool == b.Bool
	case TagNull:
		return true
	}
	return false
}

// NumCompare : comparaison numérique. null ne satisfait aucune comparaison (trivalent, ADR 0003).
func NumCompare(op Op, v, a Value) (bool, error) {
	if v.Tag == TagNull {
		return false, nil
	}
	if v.Tag != TagNumber || a.Tag != TagNumber {
		return false, fmt.Errorf("comparaison numérique sur une valeur non numérique")
	}
	c := decimal.Cmp(v.Num, a.Num)
	switch op {
	case OpLt:
		return c < 0, nil
	case OpLe:
		return c <= 0, nil
	case OpGt:
		return c > 0, nil
	case OpGe:
		return c >= 0, nil
	}
	return false, fmt.Errorf("opérateur de comparaison invalide")
}

func inRange(ct CellTest, v Value) (bool, error) {
	if v.Tag == TagNull {
		return false, nil
	}
	if v.Tag != TagNumber || ct.A.Tag != TagNumber || ct.B.Tag != TagNumber {
		return false, fmt.Errorf("intervalle sur une valeur non numérique")
	}
	lo := decimal.Cmp(v.Num, ct.A.Num)
	hi := decimal.Cmp(v.Num, ct.B.Num)
	okLo := lo > 0 || (lo == 0 && !ct.AOpen)
	okHi := hi < 0 || (hi == 0 && !ct.BOpen)
	return okLo && okHi, nil
}

// MatchCell : la cellule `ct` matche-t-elle la valeur `v` ?
// OpProg nécessite une évaluation bytecode (non géométrique) -> erreur ici ; la VM la gère.
// `Negate` (issu de `not(<test>)`) inverse le test géométrique. null reste non-matchant même
// nié (trivalent, ADR 0003 : null ne satisfait aucun test, et `not` ne le « réveille » pas).
func MatchCell(ct CellTest, v Value) (bool, error) {
	res, err := matchCellBase(ct, v)
	if err != nil {
		return false, err
	}
	if ct.Negate {
		if v.Tag == TagNull {
			return false, nil
		}
		return !res, nil
	}
	return res, nil
}

func matchCellBase(ct CellTest, v Value) (bool, error) {
	switch ct.Op {
	case OpAny:
		return true, nil
	case OpEq:
		return ValueEq(v, ct.A), nil
	case OpNe:
		return !ValueEq(v, ct.A), nil
	case OpLt, OpLe, OpGt, OpGe:
		return NumCompare(ct.Op, v, ct.A)
	case OpInRange:
		return inRange(ct, v)
	case OpInSet:
		for _, sub := range ct.Sub {
			ok, err := MatchCell(sub, v)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case OpProg:
		return false, fmt.Errorf("cellule Op=Prog nécessite une évaluation (non géométrique)")
	default:
		return false, fmt.Errorf("opérateur de cellule inconnu: %d", ct.Op)
	}
}
