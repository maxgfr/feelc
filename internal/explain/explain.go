// Package explain produit une trace de JUSTIFICATION d'une décision : la règle gagnante, les
// cellules qui la justifient (avec leur position source), et la sortie. C'est la façade de haut
// niveau (mirroir de engine.Eval) : convertit les entrées brutes en Value puis délègue à vm.Trace,
// qui REJOUE l'évaluation via la même sémantique que le moteur (aucune divergence possible).
package explain

import (
	"fmt"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/vm"
)

// Trace est la trace de justification (alias du type produit par la VM).
type Trace = vm.DecisionTrace

// Explain évalue une décision d'un modèle compilé et renvoie sa justification.
func Explain(cm *ir.CompiledModel, decision string, rawInputs map[string]any) (*Trace, error) {
	inputs := make(map[string]ir.Value, len(rawInputs))
	for k, v := range rawInputs {
		val, err := ir.FromAny(v)
		if err != nil {
			return nil, fmt.Errorf("entrée %q: %w", k, err)
		}
		inputs[k] = val
	}
	return vm.Trace(cm, decision, inputs)
}
