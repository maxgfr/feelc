// Package explain produces a JUSTIFICATION trace for a decision: the winning rule, the
// cells that justify it (with their source position), and the output. It is the high-level
// facade (mirror of engine.Eval): it converts raw inputs into Value then delegates to vm.Trace,
// which REPLAYS the evaluation using the same semantics as the engine (no divergence possible).
package explain

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/vm"
)

// Trace is the justification trace (alias of the type produced by the VM).
type Trace = vm.DecisionTrace

// Explain evaluates a decision of a compiled model and returns its justification.
func Explain(cm *ir.CompiledModel, decision string, rawInputs map[string]any) (*Trace, error) {
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
	return vm.Trace(cm, decision, inputs)
}
