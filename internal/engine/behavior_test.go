package engine_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Table with multiple input columns: all conditions in a row must match (AND).
func TestMultiColumnFirstHit(t *testing.T) {
	src := `
model "loan" {}

input score : number
input age   : number

decision verdict : string {
  needs: score, age
  hit: first
  #  score  | age   => result
     < 580   | -      => "insufficient score"
     -       | < 18   => "minor"
     >= 580  | >= 18  => "ok"
}
`
	cases := []struct {
		score, age int
		want       string
	}{
		{500, 40, "insufficient score"}, // 1st row
		{700, 16, "minor"},              // 2nd row (score ok but minor)
		{700, 40, "ok"},                 // 3rd row
	}
	for _, c := range cases {
		got, err := engine.Run(src, "verdict", map[string]any{"score": c.score, "age": c.age})
		if err != nil {
			t.Fatalf("(%d,%d): %v", c.score, c.age, err)
		}
		if got != c.want {
			t.Errorf("verdict(score=%d,age=%d) = %v, expected %q", c.score, c.age, got, c.want)
		}
	}
}

// Implicit equality on a string literal (cell = value).
func TestStringEquality(t *testing.T) {
	src := `
model "tier" {}

input plan : string

decision discount : string {
  needs: plan
  hit: first
     "gold"   => "20%"
     "silver" => "10%"
     -        => "0%"
}
`
	for _, c := range []struct{ plan, want string }{
		{"gold", "20%"}, {"silver", "10%"}, {"bronze", "0%"},
	} {
		got, err := engine.Run(src, "discount", map[string]any{"plan": c.plan})
		if err != nil {
			t.Fatalf("plan=%s: %v", c.plan, err)
		}
		if got != c.want {
			t.Errorf("discount(plan=%q) = %v, expected %q", c.plan, got, c.want)
		}
	}
}

// Scope discipline: a construct outside the v1 subset must FAIL CLEARLY
// (refuse rather than accept-then-misinterpret), as must model errors.
func TestScopeAndErrorDiscipline(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		dec     string
		wantErr string // expected substring
	}{
		{
			name: "needs to undeclared input",
			src: `model "m" {}
input a : number
decision d : string {
  needs: b
  hit: first
  - => "x"
}`,
			dec:     "d",
			wantErr: "not declared",
		},
		{
			name: "unsupported hit policy (output order deferred)",
			src: `model "m" {}
input a : number
decision d : string {
  needs: a
  hit: output order
  - => "x"
}`,
			dec:     "d",
			wantErr: "unsupported hit policy",
		},
		{
			name: "unknown decision at evaluation",
			src: `model "m" {}
input a : number
decision d : string {
  needs: a
  hit: first
  - => "x"
}`,
			dec:     "absente",
			wantErr: "unknown decision",
		},
		{
			name: "content after { on the header line",
			src: `model "m" {}
input a : number
decision d : string { needs: a
  hit: first
  - => "x"
}`,
			dec:     "d",
			wantErr: "at end of line",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := engine.Run(c.src, c.dec, map[string]any{"a": 1})
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error = %q, expected to contain %q", err.Error(), c.wantErr)
			}
		})
	}
}
