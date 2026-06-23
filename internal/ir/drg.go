package ir

import (
	"fmt"
	"sort"
)

// RequiredInputs returns the set of model inputs transitively needed to evaluate a decision —
// the backward reachability over the decision requirements graph (Decision.Deps). This powers the
// question-flow / simulator: ask the user only the inputs that actually feed the chosen goal.
// Inputs are returned sorted (deterministic). Errors if the decision is unknown.
func (cm *CompiledModel) RequiredInputs(decision string) ([]string, error) {
	if _, ok := cm.Decision(decision); !ok {
		return nil, fmt.Errorf("unknown decision %q", decision)
	}
	isDecision := make(map[string]bool, len(cm.Decisions))
	for i := range cm.Decisions {
		isDecision[cm.Decisions[i].Name] = true
	}
	seen := map[string]bool{}
	inputs := map[string]bool{}
	var visit func(name string)
	visit = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		d, ok := cm.Decision(name)
		if !ok {
			return // terminal (input / external)
		}
		for _, dep := range d.Deps {
			if isDecision[dep] {
				visit(dep)
			} else {
				inputs[dep] = true
			}
		}
	}
	visit(decision)
	out := make([]string, 0, len(inputs))
	for n := range inputs {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// RequiredDecisions returns the decisions transitively needed to evaluate a goal — the goal
// plus every upstream decision it depends on (via Decision.Deps) — in dependency-first order
// (the order Eval resolves them; the goal is last). It walks the same backward reachability as
// RequiredInputs but collects decisions, then projects them onto cm.Decisions' topological order
// for a deterministic, eval-consistent sequence. Errors if the goal is unknown.
func (cm *CompiledModel) RequiredDecisions(goal string) ([]string, error) {
	if _, ok := cm.Decision(goal); !ok {
		return nil, fmt.Errorf("unknown decision %q", goal)
	}
	need := map[string]bool{}
	var visit func(name string)
	visit = func(name string) {
		if need[name] {
			return
		}
		d, ok := cm.Decision(name)
		if !ok {
			return // input / external: not a decision
		}
		need[name] = true
		for _, dep := range d.Deps {
			visit(dep)
		}
	}
	visit(goal)
	out := make([]string, 0, len(need))
	for i := range cm.Decisions {
		if need[cm.Decisions[i].Name] {
			out = append(out, cm.Decisions[i].Name)
		}
	}
	return out, nil
}
