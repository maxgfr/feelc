package dsl_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dsl"
)

// Col doit être renseigné (1-based) pour chaque cellule, calculé au split DSL :
// la colonne pointe le 1er caractère du contenu trimé dans la ligne source.
func TestCellColumnsFilled(t *testing.T) {
	const ruleLine = "  >= 1 | < 2 => 10"
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"input b : number\n" +
		"decision d : number {\n" +
		"  needs: a, b\n" +
		"  hit: first\n" +
		ruleLine + "\n" +
		"  default => 0\n" +
		"}\n"
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	r := m.Decisions[0].Rules[0]
	if len(r.Conds) != 2 || len(r.Outputs) != 1 {
		t.Fatalf("structure inattendue: %d conds, %d outputs", len(r.Conds), len(r.Outputs))
	}
	if want := strings.Index(ruleLine, ">=") + 1; r.Conds[0].Col != want {
		t.Errorf("Conds[0].Col = %d, attendu %d", r.Conds[0].Col, want)
	}
	if want := strings.Index(ruleLine, "<") + 1; r.Conds[1].Col != want {
		t.Errorf("Conds[1].Col = %d, attendu %d", r.Conds[1].Col, want)
	}
	if want := strings.Index(ruleLine, "10") + 1; r.Outputs[0].Col != want {
		t.Errorf("Outputs[0].Col = %d, attendu %d", r.Outputs[0].Col, want)
	}
}

// ParseFile stampille le nom de fichier sur l'erreur structurée, avec un code stable
// et une suggestion exploitable.
func TestParseFileStampsStructuredError(t *testing.T) {
	_, err := dsl.ParseFile("m.rules", "model \"m\" {}\nbogus instruction\n")
	if err == nil {
		t.Fatal("erreur attendue")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("erreur non structurée: %T %v", err, err)
	}
	if de.File != "m.rules" {
		t.Errorf("File = %q, attendu m.rules", de.File)
	}
	if de.Line != 2 {
		t.Errorf("Line = %d, attendu 2", de.Line)
	}
	if de.Code != diag.CodeUnknownStmt {
		t.Errorf("Code = %q, attendu %q", de.Code, diag.CodeUnknownStmt)
	}
	if de.Suggestion == "" {
		t.Errorf("suggestion attendue non vide")
	}
	// Compat texte : sans fichier, préfixe "ligne N:" historique.
	_, err2 := dsl.Parse("model \"m\" {}\nbogus instruction\n")
	if !strings.HasPrefix(err2.Error(), "ligne 2: ") {
		t.Errorf("format texte historique attendu, obtenu %q", err2.Error())
	}
}

// Une cellule FEEL invalide produit un diag.Error de code DSL002 enveloppant la cause FEEL.
func TestInvalidFeelCellWrapped(t *testing.T) {
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"decision d : number {\n" +
		"  needs: a\n" +
		"  hit: first\n" +
		"  1 + => 1\n" +
		"}\n"
	_, err := dsl.Parse(src)
	if err == nil {
		t.Fatal("erreur attendue sur cellule FEEL invalide")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("erreur non structurée: %T", err)
	}
	if de.Code != diag.CodeFeelSyntax {
		t.Errorf("Code = %q, attendu %q", de.Code, diag.CodeFeelSyntax)
	}
	if de.Cause == nil {
		t.Errorf("la cause FEEL doit être enveloppée")
	}
}
