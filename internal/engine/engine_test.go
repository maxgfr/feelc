package engine_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Tracer bullet (Slice 1): a single rule, end-to-end, through the ENTIRE pipeline
// (dsl.Parse -> compiler.Compile -> vm.Eval). Proves the architecture holds.
// Tests the semantics (observable behavior), not the internal implementation.
func TestTracerBulletFirstHitPolicy(t *testing.T) {
	src := `
model "mini" {}

input score : number

decision eligibility : string {
  needs: score
  hit: first
  #  score  => result
     < 580   => "rejected"
     -       => "approved"
}
`
	cases := []struct {
		score int
		want  string
	}{
		{500, "rejected"}, // matches the 1st rule (< 580)
		{579, "rejected"}, // boundary: strictly < 580
		{580, "approved"}, // 580 does not match < 580 -> falls through to "-"
		{700, "approved"},
	}
	for _, c := range cases {
		got, err := engine.Run(src, "eligibility", map[string]any{"score": c.score})
		if err != nil {
			t.Fatalf("score=%d: unexpected error: %v", c.score, err)
		}
		if got != c.want {
			t.Errorf("eligibility(score=%d) = %v, expected %q", c.score, got, c.want)
		}
	}
}
