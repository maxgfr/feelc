// Package check implements the NL<->rule semantic gate, on the TOOLING side (the core stays pure).
//
// Principle (cf. plan): "the LLM proposes, the VM disposes". The AI (skill) decomposes the business text
// into atomic structured CLAIMS {decision, witness-input, expected-output}; feelc check runs
// the DETERMINISTIC VM on each witness and compares. No LLM in the final verdict.
package check

import (
	"encoding/json"
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

// Claim: a business assertion anchored on a witness point.
type Claim struct {
	Desc     string         `json:"desc,omitempty"` // original NL phrase (traceability)
	Decision string         `json:"decision"`
	Input    map[string]any `json:"input"`
	Expect   any            `json:"expect"`
}

// Status: conservative verdict.
type Status string

const (
	Supported    Status = "supported"
	Contradicted Status = "contradicted" // blocking
	Errored      Status = "error"        // evaluation failed (blocking)
)

// Verdict: the result of a claim.
type Verdict struct {
	Claim  Claim  `json:"claim"`
	Status Status `json:"status"`
	Got    any    `json:"got,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// Report: the set of verdicts.
type Report struct {
	Verdicts []Verdict `json:"verdicts"`
}

// Blockers counts the blocking verdicts (contradicted / error).
func (r *Report) Blockers() int {
	n := 0
	for _, v := range r.Verdicts {
		if v.Status != Supported {
			n++
		}
	}
	return n
}

// Check anchors each claim on the deterministic VM and produces a verdict.
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
				Detail: fmt.Sprintf("expected %v, got %v", c.Expect, jsonify(got))})
		}
	}
	return rep
}

// Equal exposes the expected-vs-got comparison (exact decimal equality, lists, contexts) —
// reused AS-IS by the TCK harness (internal/tck), zero duplication of semantics.
func Equal(expect, got any) bool { return equalValue(expect, got) }

// equalValue compares an expected value (from JSON, numbers as json.Number) to the VM
// output (exact decimals, lists, contexts).
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

// jsonify makes the VM output serializable (decimals -> numbers).
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
