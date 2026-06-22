// Package check implémente le gate sémantique NL<->règle, côté OUTILLAGE (le cœur reste pur).
//
// Principe (cf. plan) : « le LLM propose, la VM dispose ». L'IA (skill) décompose le texte métier
// en CLAIMS atomiques structurés {décision, entrée-témoin, sortie attendue} ; feelc check exécute
// la VM DÉTERMINISTE sur chaque témoin et compare. Aucun LLM dans le verdict final.
package check

import (
	"encoding/json"
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

// Claim : une affirmation métier ancrée sur un point-témoin.
type Claim struct {
	Desc     string         `json:"desc,omitempty"` // phrase NL d'origine (traçabilité)
	Decision string         `json:"decision"`
	Input    map[string]any `json:"input"`
	Expect   any            `json:"expect"`
}

// Status : verdict conservateur.
type Status string

const (
	Supported   Status = "supported"
	Contradicted Status = "contradicted" // bloquant
	Errored     Status = "error"          // l'évaluation a échoué (bloquant)
)

// Verdict : le résultat d'un claim.
type Verdict struct {
	Claim  Claim  `json:"claim"`
	Status Status `json:"status"`
	Got    any    `json:"got,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Report : l'ensemble des verdicts.
type Report struct {
	Verdicts []Verdict `json:"verdicts"`
}

// Blockers compte les verdicts bloquants (contradicted / error).
func (r *Report) Blockers() int {
	n := 0
	for _, v := range r.Verdicts {
		if v.Status != Supported {
			n++
		}
	}
	return n
}

// Check ancre chaque claim sur la VM déterministe et rend un verdict.
func Check(cm *ir.CompiledModel, claims []Claim) *Report {
	rep := &Report{}
	for _, c := range claims {
		got, err := engine.Eval(cm, c.Decision, c.Input)
		switch {
		case err != nil:
			rep.Verdicts = append(rep.Verdicts, Verdict{Claim: c, Status: Errored, Detail: err.Error()})
		case equalValue(c.Expect, got):
			rep.Verdicts = append(rep.Verdicts, Verdict{Claim: c, Status: Supported, Got: jsonify(got)})
		default:
			rep.Verdicts = append(rep.Verdicts, Verdict{Claim: c, Status: Contradicted, Got: jsonify(got),
				Detail: fmt.Sprintf("attendu %v, obtenu %v", c.Expect, jsonify(got))})
		}
	}
	return rep
}

// equalValue compare une valeur attendue (issue de JSON, nombres en json.Number) à la sortie
// de la VM (décimaux exacts, listes, contexts).
func equalValue(expect, got any) bool {
	switch e := expect.(type) {
	case nil:
		return got == nil
	case bool:
		g, ok := got.(bool)
		return ok && g == e
	case string:
		g, ok := got.(string)
		return ok && g == e
	case json.Number:
		return numEq(e.String(), got)
	case float64:
		return numEq(fmt.Sprint(e), got)
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(e) {
			return false
		}
		for i := range e {
			if !equalValue(e[i], g[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok || len(g) != len(e) {
			return false
		}
		for k, ev := range e {
			gv, ok := g[k]
			if !ok || !equalValue(ev, gv) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func numEq(expect string, got any) bool {
	g, ok := got.(*apd.Decimal)
	if !ok {
		return false
	}
	e, err := decimal.Parse(expect)
	if err != nil {
		return false
	}
	return decimal.Cmp(e, g) == 0
}

// jsonify rend la sortie VM sérialisable (décimaux -> nombres).
func jsonify(v any) any {
	switch x := v.(type) {
	case *apd.Decimal:
		return json.Number(x.Text('f'))
	case []any:
		for i := range x {
			x[i] = jsonify(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = jsonify(x[k])
		}
		return x
	default:
		return v
	}
}
