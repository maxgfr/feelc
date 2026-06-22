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

// CompileFile propagates the filename onto structured errors (parse AND compile).
func TestCompileFileStampsFilename(t *testing.T) {
	// Compilation error (invalid hit policy) -> must carry the file.
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
		t.Fatalf("unstructured error: %T %v", err, err)
	}
	if de.File != "foo.rules" {
		t.Errorf("File = %q, expected foo.rules", de.File)
	}

	// Parse error -> must also carry the file.
	_, _, _, err = loader.CompileFile("bar.rules", []byte("not a model\n"))
	if !errors.As(err, &de) {
		t.Fatalf("unstructured parse error: %T %v", err, err)
	}
	if de.File != "bar.rules" {
		t.Errorf("File (parse) = %q, expected bar.rules", de.File)
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

// Golden rule: a broken source does NOT swap — the current healthy model is kept.
func TestReloadRejectsBrokenKeepsCurrent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "m.rules")
	reg := registry.New()

	writeFile(t, p, validRules)
	e1, _, err := loader.Reload(p, reg, false)
	if err != nil {
		t.Fatal(err)
	}
	if e1.Version != 1 {
		t.Fatalf("initial version = %d, expected 1", e1.Version)
	}

	// Source that fails to compile (invalid hit policy).
	writeFile(t, p, `model "m" {}
input n : number
decision d : string {
  needs: n
  hit: bogus_policy
  - => "x"
}`)
	if _, _, err := loader.Reload(p, reg, false); err == nil {
		t.Fatal("expected error for source that does not compile")
	}
	if cur := reg.Current(); cur == nil || cur.Version != 1 {
		t.Fatalf("the current model should have stayed v1, got %+v", cur)
	}

	writeFile(t, p, validRules)
	e2, _, err := loader.Reload(p, reg, false)
	if err != nil {
		t.Fatal(err)
	}
	if e2.Version != 2 {
		t.Fatalf("after valid source, version = %d, expected 2", e2.Version)
	}
}

// Strict mode: verification blockers prevent publication.
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
		t.Fatal("strict mode: expected error (completeness blockers)")
	}
	if rep == nil || rep.Blockers() == 0 {
		t.Fatal("expected report with blockers")
	}
	if reg.Current() != nil {
		t.Fatal("nothing must be published in strict mode with blockers")
	}

	if _, _, err := loader.Reload(p, reg, false); err != nil { // non-strict publishes anyway
		t.Fatal(err)
	}
	if reg.Current() == nil {
		t.Fatal("non-strict: the model must be published despite the remarks")
	}
}
