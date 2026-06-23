package dsl_test

import (
	"strings"
	"testing"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/dsl"
)

// A `unit ` substring inside a string-enum domain must NOT be treated as a unit clause.
func TestUnitClauseDoesNotCorruptStringEnum(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input plan : string in {"unit price", "flat"}`)
	if err != nil {
		t.Fatal(err)
	}
	in := m.Inputs[0]
	if in.Unit != "" {
		t.Errorf("string enum must not pick up a unit, got %q", in.Unit)
	}
	if !strings.Contains(in.Domain, "unit price") {
		t.Errorf("domain corrupted: %q", in.Domain)
	}
}

// A numeric input keeps its unit clause.
func TestNumericUnitStillParsed(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input salary : number >= 0 unit "EUR/month"`)
	if err != nil {
		t.Fatal(err)
	}
	if m.Inputs[0].Unit != "EUR/month" || m.Inputs[0].Domain != ">= 0" {
		t.Errorf("got unit=%q domain=%q", m.Inputs[0].Unit, m.Inputs[0].Domain)
	}
}

// A whole-cell percent literal reduces ("30%" -> the exact decimal 0.3, not 34 digits).
func TestPercentLiteralReduced(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input x : number
decision d : number {
  needs: x
  hit: first
  >= 0 => 30%
}`)
	if err != nil {
		t.Fatal(err)
	}
	node := m.Decisions[0].Rules[0].Outputs[0].Node
	num, ok := node.(*feel.NumberNode)
	if !ok {
		t.Fatalf("percent cell did not produce a NumberNode: %T", node)
	}
	if num.Value != "0.3" {
		t.Errorf("30%% should reduce to 0.3, got %q", num.Value)
	}
}

// "Inf%" / "NaN%" must NOT become numeric literals (no non-finite constants in the core).
func TestPercentLiteralRejectsNonFinite(t *testing.T) {
	for _, bad := range []string{"Inf%", "NaN%"} {
		_, err := dsl.Parse(`model "m" {}
input x : number
decision d : string {
  needs: x
  hit: first
  ` + bad + ` => "a"
  -    => "b"
}`)
		if err == nil {
			t.Errorf("%q must not be accepted as a percent literal", bad)
		}
	}
}

// `bracket:` with no input name is a loud error (not a silent downgrade to a table).
func TestEmptyBracketErrors(t *testing.T) {
	_, err := dsl.Parse(`model "m" {}
input x : number >= 0
decision t : number {
  bracket:
  [0..10) => 0%
}`)
	if err == nil || !strings.Contains(err.Error(), "bracket") {
		t.Fatalf("empty bracket must error, got %v", err)
	}
}
