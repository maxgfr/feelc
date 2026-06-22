package engine_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Null/error policy (ADR 0003).

// No rule matches and no `default` -> null result (nil).
func TestNoMatchNoDefaultIsNull(t *testing.T) {
	src := `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: first
     < 0 => "neg"
}`
	got, err := engine.Run(src, "d", map[string]any{"n": 5})
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil (null), got %v (%T)", got, got)
	}
}

// A null input matches no condition -> falls to `default`, without error.
func TestNullInputFallsToDefault(t *testing.T) {
	src := `model "m" {}
input n : number
decision band : string {
  needs: n
  hit: first
     [0..10) => "low"
     default => "out"
}`
	got, err := engine.Run(src, "band", map[string]any{"n": nil})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != "out" {
		t.Errorf("null input -> expected \"out\" (default), got %v", got)
	}
}

// Arithmetic propagates null (no exception).
func TestArithmeticNullPropagates(t *testing.T) {
	src := `model "m" {}
input a : number
input b : number
decision total : number = a + b`
	got, err := engine.Run(src, "total", map[string]any{"a": nil, "b": 5})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got != nil {
		t.Errorf("a null -> expected null, got %v (%T)", got, got)
	}
}

// Division by zero is an error (undefined case, distinct from null).
func TestDivisionByZeroErrors(t *testing.T) {
	src := `model "m" {}
input a : number
input b : number
decision r : number = a / b`
	_, err := engine.Run(src, "r", map[string]any{"a": 10, "b": 0})
	if err == nil {
		t.Fatal("expected error for division by zero")
	}
	if !strings.Contains(err.Error(), "division by zero") {
		t.Errorf("error = %q, expected mention of division by zero", err.Error())
	}
}

// Missing required input -> explicit error (caller contract).
func TestMissingInputErrors(t *testing.T) {
	src := `model "m" {}
input a : number
input b : number
decision r : number = a + b`
	_, err := engine.Run(src, "r", map[string]any{"a": 10}) // b missing
	if err == nil {
		t.Fatal("expected error for missing input")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("error = %q, expected mention of unknown/missing input", err.Error())
	}
}
