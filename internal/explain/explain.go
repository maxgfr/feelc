// Package explain produces a JUSTIFICATION trace for a decision: the winning rule, the
// cells that justify it (with their source position), and the output. It is the high-level
// facade (mirror of engine.Eval): it converts raw inputs into Value then delegates to vm.Trace,
// which REPLAYS the evaluation using the same semantics as the engine (no divergence possible).
package explain

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/modelinfo"
	"github.com/maxgfr/feelc/internal/vm"
)

// Trace is the justification trace (alias of the type produced by the VM).
type Trace = vm.DecisionTrace

// FullTrace is the justification of a decision AND its whole upstream DRG path (alias of the VM type).
type FullTrace = vm.FullTrace

// coerce converts raw (JSON-ish) inputs into typed Values, applying date/duration coercion.
func coerce(cm *ir.CompiledModel, rawInputs map[string]any) (map[string]ir.Value, error) {
	inputs := make(map[string]ir.Value, len(rawInputs))
	for k, v := range rawInputs {
		val, err := ir.FromAny(v)
		if err != nil {
			return nil, fmt.Errorf("input %q: %w", k, err)
		}
		inputs[k] = val
	}
	if err := ir.CoerceInputs(cm, inputs); err != nil {
		return nil, err
	}
	return inputs, nil
}

// Explain evaluates a decision of a compiled model and returns its justification.
func Explain(cm *ir.CompiledModel, decision string, rawInputs map[string]any) (*Trace, error) {
	inputs, err := coerce(cm, rawInputs)
	if err != nil {
		return nil, err
	}
	return vm.Trace(cm, decision, inputs)
}

// ExplainFull evaluates a decision and returns the justification of the goal AND every upstream
// decision it transitively consumed, in dependency-first order (goal last).
func ExplainFull(cm *ir.CompiledModel, decision string, rawInputs map[string]any) (*FullTrace, error) {
	inputs, err := coerce(cm, rawInputs)
	if err != nil {
		return nil, err
	}
	return vm.TraceFull(cm, decision, inputs)
}

// NormalizeJSON rewrites a trace's decimal Output to a fixed-notation json.Number via modelinfo.JSONify
// (recursing into context/list outputs). Without it a *apd.Decimal serializes through its TextMarshaler
// as scientific notation (e.g. "1E+1" for 10), which would make the JSON trace inconsistent with the
// run `output` field. It mutates the (per-request) trace in place and returns it for chaining; the raw
// Explain/ExplainFull result keeps *apd.Decimal for in-process callers (CLI human output, tests).
func NormalizeJSON(tr *Trace) *Trace {
	if tr != nil {
		tr.Output = modelinfo.JSONify(tr.Output)
	}
	return tr
}

// NormalizeFullJSON applies NormalizeJSON to every decision on a full trace's path.
func NormalizeFullJSON(ft *FullTrace) *FullTrace {
	if ft == nil {
		return ft
	}
	for _, d := range ft.Path {
		NormalizeJSON(d) // ft.Result aliases Path[last]; JSONify is idempotent on the repeat
	}
	return ft
}
