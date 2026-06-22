package engine_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Tracer bullet (Tranche 1) : une règle, bout-en-bout, à travers TOUT le pipeline
// (dsl.Parse -> compiler.Compile -> vm.Eval). Prouve que l'architecture tient.
// Sémantique testée (comportement observable), pas l'implémentation interne.
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
		{500, "rejected"}, // matche la 1re règle (< 580)
		{579, "rejected"}, // borne : strictement < 580
		{580, "approved"}, // 580 ne matche pas < 580 -> tombe sur "-"
		{700, "approved"},
	}
	for _, c := range cases {
		got, err := engine.Run(src, "eligibility", map[string]any{"score": c.score})
		if err != nil {
			t.Fatalf("score=%d: erreur inattendue: %v", c.score, err)
		}
		if got != c.want {
			t.Errorf("eligibility(score=%d) = %v, attendu %q", c.score, got, c.want)
		}
	}
}
