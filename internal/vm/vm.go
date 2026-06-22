// Package vm exécute un *ir.CompiledModel de façon déterministe. En Tranche 1 :
// évaluation d'une table avec hit policy FIRST, conditions any/comparaison/égalité.
// Aucune source d'indéterminisme (pas d'horloge, pas de map-range dans la décision).
package vm

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// Eval évalue la décision nommée à partir des entrées fournies.
func Eval(cm *ir.CompiledModel, decisionName string, inputs map[string]ir.Value) (ir.Value, error) {
	dec, ok := cm.Decision(decisionName)
	if !ok {
		return ir.Value{}, fmt.Errorf("décision inconnue: %q", decisionName)
	}
	if dec.Kind != ir.KindTable {
		return ir.Value{}, fmt.Errorf("décision %q: type d'exécution non supporté en v1", decisionName)
	}
	return evalTable(dec.Table, inputs)
}

func evalTable(t *ir.DecisionTable, inputs map[string]ir.Value) (ir.Value, error) {
	switch t.HitPolicy {
	case ir.HitFirst:
		for _, r := range t.Rules {
			ok, err := matches(r, t.Inputs, inputs)
			if err != nil {
				return ir.Value{}, err
			}
			if ok {
				return r.Outputs[0], nil
			}
		}
		return ir.Null(), nil // aucun match : null (ligne `default` arrive en Tranche 2)
	default:
		return ir.Value{}, fmt.Errorf("hit policy non supportée en v1")
	}
}

func matches(r ir.Rule, cols []string, inputs map[string]ir.Value) (bool, error) {
	for i, ct := range r.Conds {
		ok, err := testCell(ct, inputs[cols[i]])
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func testCell(ct ir.CellTest, v ir.Value) (bool, error) {
	switch ct.Op {
	case ir.OpAny:
		return true, nil
	case ir.OpEq:
		return valueEq(v, ct.A), nil
	case ir.OpNe:
		return !valueEq(v, ct.A), nil
	case ir.OpLt, ir.OpLe, ir.OpGt, ir.OpGe:
		if v.Tag != ir.TagNumber || ct.A.Tag != ir.TagNumber {
			return false, fmt.Errorf("comparaison numérique sur une valeur non numérique")
		}
		c := decimal.Cmp(v.Num, ct.A.Num)
		switch ct.Op {
		case ir.OpLt:
			return c < 0, nil
		case ir.OpLe:
			return c <= 0, nil
		case ir.OpGt:
			return c > 0, nil
		default: // OpGe
			return c >= 0, nil
		}
	default:
		return false, fmt.Errorf("opérateur de cellule non supporté en v1: %d", ct.Op)
	}
}

func valueEq(a, b ir.Value) bool {
	if a.Tag != b.Tag {
		return false
	}
	switch a.Tag {
	case ir.TagNumber:
		return decimal.Cmp(a.Num, b.Num) == 0
	case ir.TagString:
		return a.Str == b.Str
	case ir.TagBool:
		return a.Bool == b.Bool
	case ir.TagNull:
		return true
	}
	return false
}
