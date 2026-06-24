package engine_test

import (
	"os"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/loader"
)

func loadSMTResidual(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../examples/smt-residual/risk.rules")
	if err != nil {
		t.Fatalf("reading risk.rules: %v", err)
	}
	return string(b)
}

// band (hit FIRST): exceeds if floor(amount) >= threshold, else at_threshold if
// amount >= threshold, else under. Cells are non-geometric (Op=Prog): they compare the
// column `?` against the input `threshold` and one applies `floor`.
func TestExampleSMTResidualBand(t *testing.T) {
	src := loadSMTResidual(t)
	cases := []struct {
		amount, threshold float64
		want              string
	}{
		{5, 3, "exceeds"},          // floor(5)=5 >= 3
		{3, 3, "exceeds"},          // floor(3)=3 >= 3
		{3.9, 3.5, "at_threshold"}, // floor(3.9)=3 < 3.5, but 3.9 >= 3.5
		{2.9, 3, "under"},          // floor(2.9)=2 < 3, 2.9 >= 3? no
		{0, 0, "exceeds"},          // floor(0)=0 >= 0  (playground default boundary)
		{1, 5, "under"},            // floor(1)=1 < 5, 1 >= 5? no
	}
	for _, c := range cases {
		out, err := engine.Run(src, "band", map[string]any{"amount": c.amount, "threshold": c.threshold})
		if err != nil {
			t.Fatalf("band(amount=%v,threshold=%v): %v", c.amount, c.threshold, err)
		}
		if out != c.want {
			t.Errorf("band(amount=%v,threshold=%v) = %v, want %q", c.amount, c.threshold, out, c.want)
		}
	}
}

// side (hit UNIQUE): below if amount < threshold, else at_or_above. The two rules tile the
// domain by a cross-column split, so the table is complete AND conflict-free.
func TestExampleSMTResidualSide(t *testing.T) {
	src := loadSMTResidual(t)
	cases := []struct {
		amount, threshold float64
		want              string
	}{
		{2, 3, "below"},
		{3, 3, "at_or_above"},
		{4, 3, "at_or_above"},
		{0, 0, "at_or_above"}, // playground default boundary
	}
	for _, c := range cases {
		out, err := engine.Run(src, "side", map[string]any{"amount": c.amount, "threshold": c.threshold})
		if err != nil {
			t.Fatalf("side(amount=%v,threshold=%v): %v", c.amount, c.threshold, err)
		}
		if out != c.want {
			t.Errorf("side(amount=%v,threshold=%v) = %v, want %q", c.amount, c.threshold, out, c.want)
		}
	}
}

// Regression: a decision table that references an input inside its cell expressions (Op=Prog)
// must report that input as required. `band`/`side` only list `amount` in `needs:`, but their
// cells also read `threshold` — so `threshold` is a genuine dependency the question-flow,
// the DRG and the playground auto-run all rely on.
func TestExampleSMTResidualRequiredInputsIncludeThreshold(t *testing.T) {
	cm, _, _, err := loader.Compile([]byte(loadSMTResidual(t)))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, goal := range []string{"band", "side"} {
		got, err := cm.RequiredInputs(goal)
		if err != nil {
			t.Fatalf("RequiredInputs(%s): %v", goal, err)
		}
		want := map[string]bool{"amount": true, "threshold": true}
		seen := map[string]bool{}
		for _, n := range got {
			seen[n] = true
		}
		for n := range want {
			if !seen[n] {
				t.Errorf("RequiredInputs(%s) = %v, missing %q", goal, got, n)
			}
		}
	}
}

// Regression for the GitHub-Pages playground "run failed (422)": the playground auto-runs the
// goal decision with one default value per RequiredInputs entry. If `threshold` is not reported
// as required, the auto-run omits it and the engine fails with "unknown variable threshold".
// Building the input set from RequiredInputs must therefore yield a runnable decision.
func TestExampleSMTResidualPlaygroundAutoRunDoesNotFail(t *testing.T) {
	cm, _, _, err := loader.Compile([]byte(loadSMTResidual(t)))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, goal := range []string{"band", "side"} {
		reqs, err := cm.RequiredInputs(goal)
		if err != nil {
			t.Fatalf("RequiredInputs(%s): %v", goal, err)
		}
		input := map[string]any{}
		for _, n := range reqs {
			input[n] = 0 // domain lower bound, as the playground's defaultInput picks
		}
		if _, err := engine.Eval(cm, goal, input); err != nil {
			t.Fatalf("playground auto-run of %q failed: %v (RequiredInputs=%v)", goal, err, reqs)
		}
	}
}
