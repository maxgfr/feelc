// Package vm executes an *ir.CompiledModel deterministically and demand-driven:
// a decision is evaluated on demand (its DRG dependencies first), with memoization
// and cycle detection. No source of nondeterminism in the decision path.
package vm

import (
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// Eval evaluates the named decision from the provided external inputs.
func Eval(cm *ir.CompiledModel, decisionName string, inputs map[string]ir.Value) (ir.Value, error) {
	if _, ok := cm.Decision(decisionName); !ok {
		return ir.Value{}, fmt.Errorf("unknown decision: %q", decisionName)
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

// resolve returns the value of a name: external input, or decision evaluated on demand.
func (e *evaluator) resolve(name string) (ir.Value, error) {
	if v, ok := e.inputs[name]; ok {
		return v, nil
	}
	dec, ok := e.cm.Decision(name)
	if !ok {
		return ir.Value{}, fmt.Errorf("unknown variable at execution time: %q (missing input?)", name)
	}
	switch e.state[name] {
	case 2:
		return e.memo[name], nil
	case 1:
		return ir.Value{}, fmt.Errorf("decision cycle detected involving %q", name)
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
		return ir.Value{}, fmt.Errorf("decision %q: unsupported execution type", d.Name)
	}
}

func (e *evaluator) evalTable(t *ir.DecisionTable) (ir.Value, error) {
	cols := make([]ir.Value, len(t.Inputs))
	for i, name := range t.Inputs {
		v, err := e.resolve(name) // external input or upstream decision
		if err != nil {
			return ir.Value{}, err
		}
		cols[i] = v
	}

	// FIRST: short-circuits on the first matching rule (order = priority).
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

	// Other policies: collect all matching rules.
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
			return ir.Value{}, fmt.Errorf("hit policy UNIQUE: %d rules match (at most 1 expected)", len(matched))
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
				return ir.Value{}, fmt.Errorf("hit policy ANY: conflicting rules (divergent outputs)")
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
		return ir.Value{}, fmt.Errorf("unsupported hit policy at execution time")
	}
}

// fallback: output of the `default` line, otherwise null (the completeness check will warn, Slice 4).
func (e *evaluator) fallback(t *ir.DecisionTable) (ir.Value, error) {
	if t.Default != nil {
		return buildOutput(t.Outputs, t.Default), nil
	}
	return ir.Null(), nil
}

// collect aggregates the matching rules (COLLECT / RULE ORDER).
func (e *evaluator) collect(t *ir.DecisionTable, matched []ir.Rule) (ir.Value, error) {
	switch t.Agg {
	case ir.AggNone: // list of outputs, in rule order
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
		return ir.Value{}, fmt.Errorf("unknown COLLECT aggregation")
	}
}

func aggregateNumbers(agg ir.Aggregation, matched []ir.Rule) (ir.Value, error) {
	if len(matched) == 0 {
		if agg == ir.AggSum {
			return ir.Num(decimal.FromInt(0)), nil
		}
		return ir.Null(), nil // min/max on empty set -> null
	}
	acc := new(apd.Decimal)
	for i, r := range matched {
		v := r.Outputs[0]
		if v.Tag != ir.TagNumber {
			return ir.Value{}, fmt.Errorf("COLLECT aggregation on a non-numeric output")
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

// rank returns the index of v in the priority list (smaller = higher priority);
// an absent value is the lowest priority.
func rank(priority []ir.Value, v ir.Value) int {
	for i, p := range priority {
		if ir.ValueEq(v, p) {
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
		if !ir.ValueEq(a[i], b[i]) {
			return false
		}
	}
	return true
}

// buildOutput yields a scalar output (1 column) or a context (>1).
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
	if ct.Op == ir.OpProg {
		res, err := e.evalExpr(ct.Prog, &v)
		if err != nil {
			return false, err
		}
		if res.Tag != ir.TagBool {
			return false, fmt.Errorf("cell Op=Prog: non-boolean result")
		}
		return res.Bool, nil
	}
	return ir.MatchCell(ct, v) // geometric semantics shared with the checker
}
