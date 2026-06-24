package eval

import "testing"

// A reference (human-authored) solution per corpus task. The harness must score these 100% — this both
// proves the scorer works and gives the live LLM driver a ground-truth baseline.
var referenceSolutions = map[string]string{
	"tiered-discount": `model "shop" {}
input amount : number
decision discount : number {
  needs: amount
  hit: first
  >= 500 => 15
  >= 100 => 10
  >= 50  => 5
  -      => 0
}`,
	"risk-band": `model "risk" {}
input score : number
decision band : string {
  needs: score
  hit: first
  >= 700 => "high"
  >= 400 => "medium"
  -      => "low"
}`,
}

// TestReferenceSolutionsScorePerfect: every reference solution compiles, verifies clean, and reproduces
// all cases — Result.OK() must hold. If this fails, either the scorer or the corpus drifted.
func TestReferenceSolutionsScorePerfect(t *testing.T) {
	for _, r := range ScoreAll(referenceSolutions, Corpus) {
		if !r.OK() {
			t.Errorf("reference solution for %q must score 100%%: %+v", r.Task, r)
		}
		if r.Passed != r.Total || r.Total == 0 {
			t.Errorf("task %q: passed %d/%d", r.Task, r.Passed, r.Total)
		}
	}
}

// TestScoreDetectsFailures: the scorer must catch a wrong candidate (some cases fail) and a
// non-compiling candidate (Compiles=false) — otherwise the measurement is meaningless.
func TestScoreDetectsFailures(t *testing.T) {
	wrong := `model "shop" {}
input amount : number
decision discount : number {
  needs: amount
  hit: first
  -      => 0
}` // always 0 -> only the amount=10 case passes
	r := Score(wrong, Corpus[0])
	if r.OK() {
		t.Errorf("an always-zero candidate must not score OK: %+v", r)
	}
	if r.Passed == r.Total {
		t.Errorf("expected failing cases for the wrong candidate, got %d/%d", r.Passed, r.Total)
	}

	broken := `model "x" {}
decision d : number {
  needs: undeclared
  hit: first
  >= 0 => 1
}` // references an undeclared input -> genuine compile error
	if r2 := Score(broken, Corpus[0]); r2.Compiles {
		t.Errorf("a non-compiling source must report Compiles=false: %+v", r2)
	}
}
