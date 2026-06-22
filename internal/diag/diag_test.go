package diag_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/diag"
)

// Error() doit rester COMPATIBLE avec le format texte historique quand aucun
// fichier n'est connu : "ligne N: <message>" (les tests existants matchent ces
// sous-chaînes FR). Aucune suggestion ne doit fuiter dans Error().
func TestErrorTextCompatNoFile(t *testing.T) {
	e := diag.New("DSL001", 3, "instruction non reconnue").WithSuggestion("essayez `input`")
	if got, want := e.Error(), "ligne 3: instruction non reconnue"; got != want {
		t.Fatalf("Error() = %q, attendu %q", got, want)
	}
	if strings.Contains(e.Error(), "essayez") {
		t.Fatalf("la suggestion ne doit pas apparaître dans Error(): %q", e.Error())
	}
}

// Avec un fichier et une colonne, Error() rend "file:line:col: message".
func TestErrorTextWithFileAndCol(t *testing.T) {
	e := diag.New("DSL002", 3, "cellule FEEL invalide").WithFile("m.rules").WithCol(5)
	if got, want := e.Error(), "m.rules:3:5: cellule FEEL invalide"; got != want {
		t.Fatalf("Error() = %q, attendu %q", got, want)
	}
}

// Col=0 (inconnue) -> "file:line: message" (pas de :0:).
func TestErrorTextWithFileNoCol(t *testing.T) {
	e := diag.New("DSL002", 3, "cellule FEEL invalide").WithFile("m.rules")
	if got, want := e.Error(), "m.rules:3: cellule FEEL invalide"; got != want {
		t.Fatalf("Error() = %q, attendu %q", got, want)
	}
}

// Line=0 (erreur sans position) -> message nu, sans préfixe.
func TestErrorTextNoLine(t *testing.T) {
	e := diag.New("DSL000", 0, `modèle sans déclaration "model"`)
	if got, want := e.Error(), `modèle sans déclaration "model"`; got != want {
		t.Fatalf("Error() = %q, attendu %q", got, want)
	}
}

// Une cause enveloppée (%w historique) est rendue après ": " et remonte via Unwrap/errors.As.
func TestWrapAppendsCauseAndUnwraps(t *testing.T) {
	cause := errors.New("unexpected token")
	e := diag.Wrap("DSL003", 5, `expression FEEL invalide "x +"`, cause)
	if got, want := e.Error(), `ligne 5: expression FEEL invalide "x +": unexpected token`; got != want {
		t.Fatalf("Error() = %q, attendu %q", got, want)
	}
	if !errors.Is(e, cause) {
		t.Fatalf("errors.Is ne retrouve pas la cause")
	}
}

// MarshalJSON produit {file,line,col,code,message,suggestion} avec omitempty
// sur file/col/suggestion ; line et message toujours présents.
func TestMarshalJSONShape(t *testing.T) {
	e := diag.New("DSL001", 3, "instruction non reconnue").
		WithFile("m.rules").WithCol(5).WithSuggestion("essayez `input`")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"file", "line", "col", "code", "message", "suggestion"} {
		if _, ok := got[k]; !ok {
			t.Errorf("champ JSON manquant: %q (json=%s)", k, b)
		}
	}
	if got["message"] != "instruction non reconnue" {
		t.Errorf("message = %v", got["message"])
	}
}

// omitempty : sans file/col/suggestion, ces champs disparaissent du JSON.
func TestMarshalJSONOmitEmpty(t *testing.T) {
	e := diag.New("DSL000", 0, "erreur globale")
	b, _ := json.Marshal(e)
	s := string(b)
	for _, k := range []string{`"file"`, `"col"`, `"suggestion"`} {
		if strings.Contains(s, k) {
			t.Errorf("champ %s ne devrait pas apparaître (omitempty): %s", k, s)
		}
	}
	// message toujours présent.
	if !strings.Contains(s, `"message"`) {
		t.Errorf("message absent: %s", s)
	}
}

// errors.As permet au CLI de récupérer la structure pour la sortie --json.
func TestErrorsAsStructured(t *testing.T) {
	var err error = diag.New("DSL001", 3, "boom").WithCol(2)
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatal("errors.As n'a pas retrouvé *diag.Error")
	}
	if de.Line != 3 || de.Col != 2 || de.Code != "DSL001" {
		t.Errorf("champs incorrects: %+v", de)
	}
}
