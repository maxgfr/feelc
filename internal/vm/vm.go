// Package vm exécute un *ir.CompiledModel de façon déterministe et demand-driven :
// une décision est évaluée à la demande (ses dépendances DRG d'abord), avec mémoization
// et détection de cycle. Aucune source d'indéterminisme dans le chemin de décision.
package vm

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// Eval évalue la décision nommée à partir des entrées externes fournies.
func Eval(cm *ir.CompiledModel, decisionName string, inputs map[string]ir.Value) (ir.Value, error) {
	if _, ok := cm.Decision(decisionName); !ok {
		return ir.Value{}, fmt.Errorf("décision inconnue: %q", decisionName)
	}
	e := &evaluator{cm: cm, inputs: inputs, memo: map[string]ir.Value{}, state: map[string]int{}}
	return e.resolve(decisionName)
}

type evaluator struct {
	cm     *ir.CompiledModel
	inputs map[string]ir.Value
	memo   map[string]ir.Value
	state  map[string]int // 0 unset, 1 computing, 2 done
}

// resolve renvoie la valeur d'un nom : entrée externe, ou décision évaluée à la demande.
func (e *evaluator) resolve(name string) (ir.Value, error) {
	if v, ok := e.inputs[name]; ok {
		return v, nil
	}
	dec, ok := e.cm.Decision(name)
	if !ok {
		return ir.Value{}, fmt.Errorf("variable inconnue à l'exécution: %q (input manquant ?)", name)
	}
	switch e.state[name] {
	case 2:
		return e.memo[name], nil
	case 1:
		return ir.Value{}, fmt.Errorf("cycle de décisions détecté impliquant %q", name)
	}
	e.state[name] = 1
	v, err := e.evalDecision(dec)
	if err != nil {
		return ir.Value{}, err
	}
	e.memo[name] = v
	e.state[name] = 2
	return v, nil
}

func (e *evaluator) evalDecision(d *ir.Decision) (ir.Value, error) {
	switch d.Kind {
	case ir.KindTable:
		return e.evalTable(d.Table)
	case ir.KindLiteralExpr:
		return e.evalExpr(d.Expr, nil)
	default:
		return ir.Value{}, fmt.Errorf("décision %q: type d'exécution non supporté", d.Name)
	}
}

func (e *evaluator) evalTable(t *ir.DecisionTable) (ir.Value, error) {
	cols := make([]ir.Value, len(t.Inputs))
	for i, name := range t.Inputs {
		v, err := e.resolve(name) // entrée externe ou décision amont
		if err != nil {
			return ir.Value{}, err
		}
		cols[i] = v
	}
	switch t.HitPolicy {
	case ir.HitFirst:
		for _, r := range t.Rules {
			ok, err := e.matches(r, cols)
			if err != nil {
				return ir.Value{}, err
			}
			if ok {
				return buildOutput(t.Outputs, r.Outputs), nil
			}
		}
		if t.Default != nil {
			return buildOutput(t.Outputs, t.Default), nil
		}
		return ir.Null(), nil // aucun match, pas de défaut : null (la vérif de complétude alertera, Tranche 4)
	default:
		return ir.Value{}, fmt.Errorf("hit policy non supportée en v2")
	}
}

// buildOutput rend une sortie scalaire (1 colonne) ou un context (>1).
func buildOutput(names []string, vals []ir.Value) ir.Value {
	if len(names) == 1 {
		return vals[0]
	}
	m := make(map[string]ir.Value, len(names))
	for i, n := range names {
		m[n] = vals[i]
	}
	return ir.Ctx(m)
}

func (e *evaluator) matches(r ir.Rule, cols []ir.Value) (bool, error) {
	for i, ct := range r.Conds {
		ok, err := e.testCell(ct, cols[i])
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func (e *evaluator) testCell(ct ir.CellTest, v ir.Value) (bool, error) {
	switch ct.Op {
	case ir.OpAny:
		return true, nil
	case ir.OpEq:
		return valueEq(v, ct.A), nil
	case ir.OpNe:
		return !valueEq(v, ct.A), nil
	case ir.OpLt, ir.OpLe, ir.OpGt, ir.OpGe:
		return numCompare(ct.Op, v, ct.A)
	case ir.OpInRange:
		return inRange(ct, v)
	case ir.OpInSet:
		for _, sub := range ct.Sub {
			ok, err := e.testCell(sub, v)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	case ir.OpProg:
		res, err := e.evalExpr(ct.Prog, &v)
		if err != nil {
			return false, err
		}
		if res.Tag != ir.TagBool {
			return false, fmt.Errorf("cellule Op=Prog: résultat non booléen")
		}
		return res.Bool, nil
	default:
		return false, fmt.Errorf("opérateur de cellule non supporté: %d", ct.Op)
	}
}

func numCompare(op ir.Op, v, a ir.Value) (bool, error) {
	if v.Tag == ir.TagNull {
		return false, nil // null ne satisfait aucune comparaison (trivalent, cf. ADR 0003)
	}
	if v.Tag != ir.TagNumber || a.Tag != ir.TagNumber {
		return false, fmt.Errorf("comparaison numérique sur une valeur non numérique")
	}
	c := decimal.Cmp(v.Num, a.Num)
	switch op {
	case ir.OpLt:
		return c < 0, nil
	case ir.OpLe:
		return c <= 0, nil
	case ir.OpGt:
		return c > 0, nil
	default: // OpGe
		return c >= 0, nil
	}
}

func inRange(ct ir.CellTest, v ir.Value) (bool, error) {
	if v.Tag == ir.TagNull {
		return false, nil // null hors de tout intervalle (trivalent, cf. ADR 0003)
	}
	if v.Tag != ir.TagNumber || ct.A.Tag != ir.TagNumber || ct.B.Tag != ir.TagNumber {
		return false, fmt.Errorf("intervalle sur une valeur non numérique")
	}
	lo := decimal.Cmp(v.Num, ct.A.Num)
	hi := decimal.Cmp(v.Num, ct.B.Num)
	okLo := lo > 0 || (lo == 0 && !ct.AOpen)
	okHi := hi < 0 || (hi == 0 && !ct.BOpen)
	return okLo && okHi, nil
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
