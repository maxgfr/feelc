package verify_test

import (
	"os"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/verify"
)

func compile(t *testing.T, src string) *ir.CompiledModel {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return cm
}

func has(rep *verify.Report, kind verify.Kind) *verify.Finding {
	for i := range rep.Findings {
		if rep.Findings[i].Kind == kind {
			return &rep.Findings[i]
		}
	}
	return nil
}

// L'exemple crédit est PROUVÉ complet et sans conflit (FIRST) ; sa ligne `default` est inutile.
func TestVerifyCreditIsComplete(t *testing.T) {
	b, err := os.ReadFile("../../examples/credit/credit.rules")
	if err != nil {
		t.Fatal(err)
	}
	rep := verify.Verify(compile(t, string(b)))
	if n := rep.Blockers(); n != 0 {
		t.Errorf("crédit : %d bloqueurs, attendu 0 (table prouvée complète). Findings: %+v", n, rep.Findings)
	}
	if has(rep, verify.KindGap) != nil {
		t.Errorf("crédit : trou de complétude inattendu. Findings: %+v", rep.Findings)
	}
	if has(rep, verify.KindUnreachableDefault) == nil {
		t.Errorf("crédit : la ligne `default` devrait être détectée comme inutile (table complète)")
	}
}

// Trou de complétude : [30..60) non couvert, pas de default -> erreur + contre-exemple.
func TestVerifyDetectsGap(t *testing.T) {
	rep := verify.Verify(compile(t, `model "g" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: first
     [0..30)   => "low"
     [60..100] => "high"
}`))
	g := has(rep, verify.KindGap)
	if g == nil {
		t.Fatalf("trou attendu, findings: %+v", rep.Findings)
	}
	if g.Severity != verify.SevError {
		t.Errorf("trou sans default -> sévérité error, obtenu %s", g.Severity)
	}
	if g.Witness["n"] == "" {
		t.Errorf("contre-exemple attendu pour n, obtenu %+v", g.Witness)
	}
}

// UNIQUE avec chevauchement -> conflit bloquant.
func TestVerifyDetectsUniqueConflict(t *testing.T) {
	rep := verify.Verify(compile(t, `model "u" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: unique
     >= 0  => "a"
     >= 50 => "b"
}`))
	c := has(rep, verify.KindConflict)
	if c == nil || c.Severity != verify.SevError {
		t.Fatalf("conflit UNIQUE bloquant attendu, findings: %+v", rep.Findings)
	}
}

// FIRST : une règle dont tous les cas sont déjà couverts par une règle antérieure est masquée.
func TestVerifyDetectsMaskedRule(t *testing.T) {
	rep := verify.Verify(compile(t, `model "m" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: first
     >= 0  => "all"
     >= 50 => "fifty"
}`))
	if has(rep, verify.KindDeadRule) == nil {
		t.Errorf("règle masquée attendue, findings: %+v", rep.Findings)
	}
}

// Dégradation honnête : une cellule Op=Prog rend la table non prouvable géométriquement.
func TestVerifyDegradesOnProgCell(t *testing.T) {
	rep := verify.Verify(compile(t, `model "p" {}
input a : number
input b : number
decision d : string {
  needs: a, b
  hit: first
     > b | - => "over"
     -   | - => "ok"
}`))
	if has(rep, verify.KindNotVerifiable) == nil {
		t.Errorf("dégradation 'non vérifiable' attendue (cellule Op=Prog), findings: %+v", rep.Findings)
	}
}
