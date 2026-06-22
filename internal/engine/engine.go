// Package engine est la façade qui câble le pipeline complet de feelc :
// dsl.Parse -> compiler.Compile -> vm.Eval. C'est le point d'entrée de haut niveau
// utilisé par le CLI et par les tests d'intégration.
package engine

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/vm"
)

// Run compile une source .rules et évalue une décision sur des entrées brutes.
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

// Eval évalue une décision d'un modèle DÉJÀ COMPILÉ sur des entrées brutes (JSON-ish).
// C'est le point d'entrée du service (pas de recompilation par requête).
func Eval(cm *ir.CompiledModel, decision string, rawInputs map[string]any) (any, error) {
	inputs := make(map[string]ir.Value, len(rawInputs))
	for k, v := range rawInputs {
		val, err := ir.FromAny(v)
		if err != nil {
			return nil, fmt.Errorf("entrée %q: %w", k, err)
		}
		inputs[k] = val
	}
	out, err := vm.Eval(cm, decision, inputs)
	if err != nil {
		return nil, err
	}
	return out.ToAny(), nil
}
