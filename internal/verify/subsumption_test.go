package verify_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/verify"
)

// Subsumption : règle #2 (>= 50) incluse dans #1 (>= 0) avec la MÊME sortie -> redondante.
// Sous ANY, ce chevauchement à sorties identiques n'est PAS un conflit ; il était donc
// silencieux avant. Subsumption le signale (supprimable).
func TestVerifyDetectsRedundantSubsumedRule(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision d : string {
  needs: x
  hit: any
  >= 0  => "a"
  >= 50 => "a"
}`))
	f := has(rep, verify.KindSubsumed)
	if f == nil {
		t.Fatalf("subsumption attendue (règle #2 incluse dans #1, même sortie). Findings: %+v", rep.Findings)
	}
	if len(f.Rules) == 0 || f.Rules[0] != 2 {
		t.Errorf("règle redondante attendue = #2, obtenu %+v", f.Rules)
	}
}

// Règles disjointes : aucune subsomption.
func TestVerifyNoSubsumptionWhenDisjoint(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision d : string {
  needs: x
  hit: any
  [0..50)   => "a"
  [50..100] => "b"
}`))
	if f := has(rep, verify.KindSubsumed); f != nil {
		t.Errorf("aucune subsomption attendue (règles disjointes), obtenu %+v", f)
	}
}

// Subsumption ne double PAS un conflit déjà signalé : sous UNIQUE, le chevauchement est déjà
// un conflit, on ne ré-émet pas de KindSubsumed.
func TestVerifyNoSubsumptionUnderUnique(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input x : number in [0..100]
decision d : string {
  needs: x
  hit: unique
  >= 0  => "a"
  >= 50 => "a"
}`))
	if f := has(rep, verify.KindSubsumed); f != nil {
		t.Errorf("UNIQUE : pas de subsomption (déjà un conflit), obtenu %+v", f)
	}
	if has(rep, verify.KindConflict) == nil {
		t.Errorf("UNIQUE : un conflit de chevauchement était attendu")
	}
}
