// Package vm exécute un *ir.CompiledModel de façon déterministe et demand-driven :
// une décision est évaluée à la demande (ses dépendances DRG d'abord), avec mémoization
// et détection de cycle. Aucune source d'indéterminisme dans le chemin de décision.
package vm

import (
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

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

	// FIRST : court-circuite à la première règle qui matche (ordre = priorité).
	if t.HitPolicy == ir.HitFirst {
		for _, r := range t.Rules {
			ok, err := e.matches(r, cols)
			if err != nil {
				return ir.Value{}, err
			}
			if ok {
				return buildOutput(t.Outputs, r.Outputs), nil
			}
		}
		return e.fallback(t)
	}

	// Autres politiques : collecter toutes les règles qui matchent.
	var matched []ir.Rule
	for _, r := range t.Rules {
		ok, err := e.matches(r, cols)
		if err != nil {
			return ir.Value{}, err
		}
		if ok {
			matched = append(matched, r)
		}
	}

	switch t.HitPolicy {
	case ir.HitUnique:
		if len(matched) > 1 {
			return ir.Value{}, fmt.Errorf("hit policy UNIQUE: %d règles matchent (au plus 1 attendue)", len(matched))
		}
		if len(matched) == 1 {
			return buildOutput(t.Outputs, matched[0].Outputs), nil
		}
		return e.fallback(t)
	case ir.HitAny:
		if len(matched) == 0 {
			return e.fallback(t)
		}
		for i := 1; i < len(matched); i++ {
			if !outputsEqual(matched[i].Outputs, matched[0].Outputs) {
				return ir.Value{}, fmt.Errorf("hit policy ANY: règles en conflit (sorties divergentes)")
			}
		}
		return buildOutput(t.Outputs, matched[0].Outputs), nil
	case ir.HitPriority:
		if len(matched) == 0 {
			return e.fallback(t)
		}
		best, bestRank := matched[0], rank(t.Priority, matched[0].Outputs[0])
		for _, r := range matched[1:] {
			if rk := rank(t.Priority, r.Outputs[0]); rk < bestRank {
				best, bestRank = r, rk
			}
		}
		return buildOutput(t.Outputs, best.Outputs), nil
	case ir.HitCollect, ir.HitRuleOrder:
		return e.collect(t, matched)
	default:
		return ir.Value{}, fmt.Errorf("hit policy non supportée à l'exécution")
	}
}

// fallback : sortie de la ligne `default`, sinon null (la vérif de complétude alertera, Tranche 4).
func (e *evaluator) fallback(t *ir.DecisionTable) (ir.Value, error) {
	if t.Default != nil {
		return buildOutput(t.Outputs, t.Default), nil
	}
	return ir.Null(), nil
}

// collect agrège les règles qui matchent (COLLECT / RULE ORDER).
func (e *evaluator) collect(t *ir.DecisionTable, matched []ir.Rule) (ir.Value, error) {
	switch t.Agg {
	case ir.AggNone: // liste des sorties, dans l'ordre des règles
		xs := make([]ir.Value, 0, len(matched))
		for _, r := range matched {
			xs = append(xs, buildOutput(t.Outputs, r.Outputs))
		}
		return ir.List(xs), nil
	case ir.AggCount:
		return ir.Num(decimal.FromInt(int64(len(matched)))), nil
	case ir.AggSum, ir.AggMin, ir.AggMax:
		return aggregateNumbers(t.Agg, matched)
	default:
		return ir.Value{}, fmt.Errorf("agrégation COLLECT inconnue")
	}
}

func aggregateNumbers(agg ir.Aggregation, matched []ir.Rule) (ir.Value, error) {
	if len(matched) == 0 {
		if agg == ir.AggSum {
			return ir.Num(decimal.FromInt(0)), nil
		}
		return ir.Null(), nil // min/max sur ensemble vide -> null
	}
	acc := new(apd.Decimal)
	for i, r := range matched {
		v := r.Outputs[0]
		if v.Tag != ir.TagNumber {
			return ir.Value{}, fmt.Errorf("agrégation COLLECT sur une sortie non numérique")
		}
		if i == 0 {
			acc.Set(v.Num)
			continue
		}
		switch agg {
		case ir.AggSum:
			if _, err := decimal.Ctx.Add(acc, acc, v.Num); err != nil {
				return ir.Value{}, err
			}
		case ir.AggMin:
			if decimal.Cmp(v.Num, acc) < 0 {
				acc.Set(v.Num)
			}
		case ir.AggMax:
			if decimal.Cmp(v.Num, acc) > 0 {
				acc.Set(v.Num)
			}
		}
	}
	return ir.Num(acc), nil
}

// rank renvoie l'indice de v dans la liste de priorité (plus petit = plus prioritaire) ;
// une valeur absente est la moins prioritaire.
func rank(priority []ir.Value, v ir.Value) int {
	for i, p := range priority {
		if valueEq(v, p) {
			return i
		}
	}
	return len(priority)
}

func outputsEqual(a, b []ir.Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !valueEq(a[i], b[i]) {
			return false
		}
	}
	return true
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
