package loader_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/registry"
)

// CompileFile propage le nom de fichier sur les erreurs structurées (parse ET compile).
func TestCompileFileStampsFilename(t *testing.T) {
	// Erreur de compilation (hit policy invalide) -> doit porter le fichier.
	brokenCompile := `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: bogus_policy
  - => "x"
}`
	_, _, _, err := loader.CompileFile("foo.rules", []byte(brokenCompile))
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("erreur non structurée: %T %v", err, err)
	}
	if de.File != "foo.rules" {
		t.Errorf("File = %q, attendu foo.rules", de.File)
	}

	// Erreur de parse -> doit aussi porter le fichier.
	_, _, _, err = loader.CompileFile("bar.rules", []byte("not a model\n"))
	if !errors.As(err, &de) {
		t.Fatalf("erreur de parse non structurée: %T %v", err, err)
	}
	if de.File != "bar.rules" {
		t.Errorf("File (parse) = %q, attendu bar.rules", de.File)
	}
}

const validRules = `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: first
  < 0 => "neg"
  -   => "pos"
}`

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Règle d'or : une source cassée NE swappe PAS — le modèle courant sain est conservé.
func TestReloadRejectsBrokenKeepsCurrent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.rules")
	reg := registry.New()

	writeFile(t, p, validRules)
	e1, _, err := loader.Reload(p, reg, false)
	if err != nil {
		t.Fatal(err)
	}
	if e1.Version != 1 {
		t.Fatalf("version initiale = %d, attendu 1", e1.Version)
	}

	// Source qui échoue à la compilation (hit policy invalide).
	writeFile(t, p, `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: bogus_policy
  - => "x"
}`)
	if _, _, err := loader.Reload(p, reg, false); err == nil {
		t.Fatal("erreur attendue pour source qui ne compile pas")
	}
	if cur := reg.Current(); cur == nil || cur.Version != 1 {
		t.Fatalf("le modèle courant aurait dû rester v1, obtenu %+v", cur)
	}

	writeFile(t, p, validRules)
	e2, _, err := loader.Reload(p, reg, false)
	if err != nil {
		t.Fatal(err)
	}
	if e2.Version != 2 {
		t.Fatalf("après source valide, version = %d, attendu 2", e2.Version)
	}
}

// Mode strict : des bloqueurs de vérification empêchent la publication.
func TestStrictRejectsVerifyBlockers(t *testing.T) {
	p := filepath.Join(t.TempDir(), "g.rules")
	gap := `model "g" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: first
  [0..30) => "low"
}`
	writeFile(t, p, gap)
	reg := registry.New()

	_, rep, err := loader.Reload(p, reg, true) // strict
	if err == nil {
		t.Fatal("mode strict : erreur attendue (bloqueurs de complétude)")
	}
	if rep == nil || rep.Blockers() == 0 {
		t.Fatal("rapport avec bloqueurs attendu")
	}
	if reg.Current() != nil {
		t.Fatal("rien ne doit être publié en mode strict avec bloqueurs")
	}

	if _, _, err := loader.Reload(p, reg, false); err != nil { // non-strict publie quand même
		t.Fatal(err)
	}
	if reg.Current() == nil {
		t.Fatal("non-strict : le modèle doit être publié malgré les remarques")
	}
}
