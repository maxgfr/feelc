// Package engine is the facade that wires up the complete feelc pipeline:
// dsl.Parse -> compiler.Compile -> vm.Eval. It is the high-level entry point
// used by the CLI and by integration tests.
package engine

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/vm"
)

// Run compiles a .rules source and evaluates a decision against raw inputs.
func Run(src, decision string, rawInputs map[string]any) (any, error) {
	m, err := dsl.Parse(src)
	if err != nil {
		return nil, err
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		return nil, err
	}
	return Eval(cm, decision, rawInputs)
}

// Eval evaluates a decision of an ALREADY COMPILED model against raw inputs (JSON-ish).
// It is the service entry point (no per-request recompilation).
func Eval(cm *ir.CompiledModel, decision string, rawInputs map[string]any) (any, error) {
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
	out, err := vm.Eval(cm, decision, inputs)
	if err != nil {
		return nil, err
	}
	return out.ToAny(), nil
}
