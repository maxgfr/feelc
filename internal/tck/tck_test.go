package tck_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/tck"
)

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
