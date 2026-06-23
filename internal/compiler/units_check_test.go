package compiler_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
)

// units compiles src and returns the per-name canonical units (cm.Units).
func unitsOf(t *testing.T, src string) map[string]string {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cm.Units
}

func TestUnitsPropagate(t *testing.T) {
	u := unitsOf(t, `model "m" {}
input base : number >= 0 unit "EUR"
input extra : number >= 0 unit "EUR"
decision total : number = base + extra`)
	if u["total"] != "EUR" {
		t.Errorf("total unit = %q, want EUR", u["total"])
	}
}

func TestUnitsMulDiv(t *testing.T) {
	u := unitsOf(t, `model "m" {}
input price : number >= 0 unit "EUR"
input qty : number >= 0 unit "item"
decision revenue : number = price * qty`)
	if u["revenue"] != "EUR.item" {
		t.Errorf("revenue unit = %q, want EUR.item", u["revenue"])
	}
}

func TestUnitMismatchRejected(t *testing.T) {
	err := compileSrc(t, `model "m" {}
input a : number unit "EUR"
input b : number unit "month"
decision bad : number = a + b`)
	if err == nil || !strings.Contains(err.Error(), "unit mismatch") {
		t.Fatalf("expected a unit mismatch, got %v", err)
	}
}

func TestUnitDimensionlessConstantsOK(t *testing.T) {
	// Adding a dimensionless constant to a EUR value is allowed (constants are unit-neutral).
	u := unitsOf(t, `model "m" {}
input fee : number unit "EUR"
decision total : number = fee + 10`)
	if u["total"] != "EUR" {
		t.Errorf("total unit = %q, want EUR", u["total"])
	}
}
