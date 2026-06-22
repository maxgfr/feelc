package compiler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dsl"
)

func compileSrc(t *testing.T, src string) error {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = compiler.Compile(m)
	return err
}

// Une référence `needs` non déclarée doit produire un diag.Error positionné (ligne de la
// décision) et de code stable CMP001, en conservant la sous-chaîne "non déclaré".
func TestNeedsUndeclaredStructured(t *testing.T) {
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"decision d : string {\n" + // ligne 3
		"  needs: b\n" +
		"  hit: first\n" +
		"  - => \"x\"\n" +
		"}\n"
	err := compileSrc(t, src)
	if err == nil {
		t.Fatal("erreur attendue")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("erreur non structurée: %T %v", err, err)
	}
	if de.Code != diag.CodeUndeclared {
		t.Errorf("Code = %q, attendu %q", de.Code, diag.CodeUndeclared)
	}
	if de.Line != 3 {
		t.Errorf("Line = %d, attendu 3 (ligne de la décision)", de.Line)
	}
	if !strings.Contains(err.Error(), "non déclaré") {
		t.Errorf("la sous-chaîne historique \"non déclaré\" doit être préservée: %q", err.Error())
	}
	if de.Suggestion == "" {
		t.Errorf("une suggestion (noms valides) est attendue")
	}
}

// Une hit policy non supportée doit produire CMP002 et conserver "hit policy non supportée".
func TestHitPolicyUnsupportedStructured(t *testing.T) {
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"decision d : string {\n" +
		"  needs: a\n" +
		"  hit: output order\n" +
		"  - => \"x\"\n" +
		"}\n"
	err := compileSrc(t, src)
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("erreur non structurée: %T %v", err, err)
	}
	if de.Code != diag.CodeHitPolicy {
		t.Errorf("Code = %q, attendu %q", de.Code, diag.CodeHitPolicy)
	}
	if !strings.Contains(err.Error(), "hit policy non supportée") {
		t.Errorf("sous-chaîne historique attendue: %q", err.Error())
	}
}
