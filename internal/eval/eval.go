// Package eval is a deterministic harness for measuring LLM rule-authoring quality. An LLM (off the
// execution path, ADR 0008) turns a natural-language prompt into a `.rules` model; this package scores
// that model objectively: does it COMPILE, does it VERIFY with zero blockers, and does it REPRODUCE a
// frozen set of reference cases? Score itself is pure — the only nondeterminism in the overall loop is
// the LLM that produced the candidate — so an authoring pipeline's first-try success rate and
// repair-rounds-to-green become measurable numbers, not vibes.
package eval

import (
	"encoding/json"
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/modelinfo"
)

// Case is a reference assertion: evaluating Decision on Input must render to Expect.
type Case struct {
	Decision string         `json:"decision"`
	Input    map[string]any `json:"input"`
	Expect   string         `json:"expect"` // canonical rendered output (see render)
}

// Task is one authoring challenge: a prompt an LLM is asked to model, plus the reference cases the
// resulting model must satisfy.
type Task struct {
	Name   string `json:"name"`
	Prompt string `json:"prompt"`
	Cases  []Case `json:"cases"`
}

// Result is the deterministic score of one candidate model against one task.
type Result struct {
	Task     string   `json:"task"`
	Compiles bool     `json:"compiles"`
	Blockers int      `json:"blockers"`
	Passed   int      `json:"passed"`
	Total    int      `json:"total"`
	Errors   []string `json:"errors,omitempty"`
}

// OK reports a fully successful authoring outcome: compiles, no verification blockers, all cases pass.
func (r Result) OK() bool { return r.Compiles && r.Blockers == 0 && r.Passed == r.Total }

// Score evaluates a candidate `.rules` model against a task. Pure and deterministic: same input ⇒ same
// result. A compile failure short-circuits (the model cannot be run); otherwise every case is checked.
func Score(rules string, t Task) Result {
	res := Result{Task: t.Name, Total: len(t.Cases)}
	cm, _, rep, err := loader.Compile([]byte(rules))
	if err != nil {
		res.Errors = append(res.Errors, "compile: "+err.Error())
		return res
	}
	res.Compiles = true
	res.Blockers = rep.Blockers()
	for _, c := range t.Cases {
		got, err := engine.Eval(cm, c.Decision, c.Input)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: eval error: %v", c.Decision, err))
			continue
		}
		if g := render(got); g == c.Expect {
			res.Passed++
		} else {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: got %q, want %q", c.Decision, g, c.Expect))
		}
	}
	return res
}

// ScoreAll scores a candidate against every task it provides a name match for (by Task.Name).
// Convenience for a batch run; callers usually Score one candidate per task.
func ScoreAll(candidates map[string]string, tasks []Task) []Result {
	out := make([]Result, 0, len(tasks))
	for _, t := range tasks {
		c, ok := candidates[t.Name]
		if !ok {
			out = append(out, Result{Task: t.Name, Total: len(t.Cases), Errors: []string{"no candidate produced"}})
			continue
		}
		out = append(out, Score(c, t))
	}
	return out
}

// render produces the canonical string form of an engine output, matching how reference cases are
// written: exact-decimal text for numbers, the string itself, true/false for booleans, null, and JSON
// for lists/contexts.
func render(v any) string {
	switch x := v.(type) {
	case *apd.Decimal:
		return x.Text('f')
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		b, _ := json.Marshal(modelinfo.JSONify(v))
		return string(b)
	}
}
