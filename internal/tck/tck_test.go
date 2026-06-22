package tck_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/tck"
)

// Régression (revue adverse) : une erreur d'exécution sur un modèle COMPILÉ (ex: division par
// zéro) est une NON-CONFORMITÉ → FAIL, pas un skip (sinon on gonfle le % en silence). Seules les
// dépendances non câblées par l'import sont skippées.
func TestRunClassifiesEvalErrorAsFail(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dz")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dmn := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/" name="dz">
  <decision name="r"><variable typeRef="number"/><literalExpression><text>1 / 0</text></literalExpression></decision>
</definitions>`
	test := `<?xml version="1.0" encoding="UTF-8"?>
<testCases xmlns="http://www.omg.org/spec/DMN/20160719/testcase" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <testCase id="c1"><resultNode name="r"><expected><value xsi:type="xsd:integer">5</value></expected></resultNode></testCase>
</testCases>`
	if err := os.WriteFile(filepath.Join(dir, "dz.dmn"), []byte(dmn), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dz-test-01.xml"), []byte(test), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := tck.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 || rep.Skipped != 0 {
		t.Errorf("division par zéro = NON-CONFORMITÉ : attendu 1 fail / 0 skip, obtenu %d fail / %d skip ; %+v",
			rep.Failed, rep.Skipped, rep.Cases)
	}
}

// Décodeur de valeur TCK : exactitude décimale (json.Number), types de base, et skip honnête
// des types non supportés.
func TestRunGradeFixture(t *testing.T) {
	rep, err := tck.Run("../../testdata/dmn-tck")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 3 {
		t.Errorf("passed = %d, attendu 3 (F/B/A) ; rapport=%+v", rep.Passed, rep.Cases)
	}
	if rep.Failed != 0 {
		t.Errorf("failed = %d, attendu 0 ; rapport=%+v", rep.Failed, rep.Cases)
	}
	if rep.Skipped != 1 {
		t.Errorf("skipped = %d, attendu 1 (valeur date) ; rapport=%+v", rep.Skipped, rep.Cases)
	}
	if c := rep.Conformance(); c != 100 {
		t.Errorf("conformité = %.1f, attendu 100 (les skips ne comptent pas)", c)
	}
	// Le skip porte une raison (jamais silencieux).
	var skipReason string
	for _, c := range rep.Cases {
		if c.Status == tck.Skipped {
			skipReason = c.Reason
		}
	}
	if skipReason == "" || !strings.Contains(skipReason, "date") {
		t.Errorf("le skip doit mentionner le type date, obtenu %q", skipReason)
	}
}

// Le rapport est sérialisable en JSON (consommé par --json).
func TestReportJSON(t *testing.T) {
	rep, err := tck.Run("../../testdata/dmn-tck")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"passed"`) || !strings.Contains(string(b), `"cases"`) {
		t.Errorf("JSON inattendu: %s", b)
	}
}
