package main

import (
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Prouve que la lecture des entrées préserve l'EXACTITUDE : 2^53+1 n'est pas représentable
// exactement en float64. Si decodeInputs passait par float64, l'égalité échouerait ("miss").
// Avec UseNumber + décimal exact, elle réussit ("exact").
func TestDecodeInputsExactBeyondFloat64(t *testing.T) {
	in, err := decodeInputs(`{"score": 9007199254740993}`) // 2^53 + 1
	if err != nil {
		t.Fatal(err)
	}
	src := `model "m" {}
input score : number
decision d : string {
  needs: score
  hit: first
  9007199254740993 => "exact"
  -                => "miss"
}`
	got, err := engine.Run(src, "d", in)
	if err != nil {
		t.Fatal(err)
	}
	if got != "exact" {
		t.Errorf("got %v, attendu \"exact\" (perte de précision = entrée passée par float64)", got)
	}
}
