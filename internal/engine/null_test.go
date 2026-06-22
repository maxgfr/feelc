package engine_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Politique null/erreur (ADR 0003).

// Aucune règle ne matche et pas de `default` -> résultat null (nil).
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
		t.Errorf("attendu nil (null), obtenu %v (%T)", got, got)
	}
}

// Une entrée null ne matche aucune condition -> tombe sur le `default`, sans erreur.
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
		t.Fatalf("inattendu: %v", err)
	}
	if got != "out" {
		t.Errorf("null input -> attendu \"out\" (default), obtenu %v", got)
	}
}

// L'arithmétique propage null (pas d'exception).
func TestArithmeticNullPropagates(t *testing.T) {
	src := `model "m" {}
input a : number
input b : number
decision total : number = a + b`
	got, err := engine.Run(src, "total", map[string]any{"a": nil, "b": 5})
	if err != nil {
		t.Fatalf("inattendu: %v", err)
	}
	if got != nil {
		t.Errorf("a null -> attendu null, obtenu %v (%T)", got, got)
	}
}

// La division par zéro est une erreur (cas indéfini, distinct de null).
func TestDivisionByZeroErrors(t *testing.T) {
	src := `model "m" {}
input a : number
input b : number
decision r : number = a / b`
	_, err := engine.Run(src, "r", map[string]any{"a": 10, "b": 0})
	if err == nil {
		t.Fatal("erreur attendue pour division par zéro")
	}
	if !strings.Contains(err.Error(), "division par zéro") {
		t.Errorf("erreur = %q, attendu mention de division par zéro", err.Error())
	}
}

// Entrée requise manquante -> erreur explicite (contrat appelant).
func TestMissingInputErrors(t *testing.T) {
	src := `model "m" {}
input a : number
input b : number
decision r : number = a + b`
	_, err := engine.Run(src, "r", map[string]any{"a": 10}) // b manquant
	if err == nil {
		t.Fatal("erreur attendue pour input manquant")
	}
	if !strings.Contains(err.Error(), "inconnue") {
		t.Errorf("erreur = %q, attendu mention d'entrée inconnue/manquante", err.Error())
	}
}
