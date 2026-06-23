package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/maxgfr/feelc/internal/loader"
)

const creditPath = "../../examples/credit/credit.rules"

// TestSingleFileProjectHashMatchesLoader is the acceptance gate for the whole feature: loading a lone
// .rules file as a project must produce the SAME canonical hash as compiling it directly. If linking
// ever renumbers lines or prefixes a single module's names, this breaks (ADR 0006 hash invariant).
func TestSingleFileProjectHashMatchesLoader(t *testing.T) {
	src, err := os.ReadFile(creditPath)
	if err != nil {
		t.Fatal(err)
	}
	_, wantHash, _, err := loader.Compile(src)
	if err != nil {
		t.Fatalf("loader.Compile: %v", err)
	}

	p, err := Load(creditPath)
	if err != nil {
		t.Fatalf("Load(file): %v", err)
	}
	if p.Hash != wantHash {
		t.Fatalf("project hash %s != loader hash %s (link must be the identity for one module)", p.Hash, wantHash)
	}
	if len(p.Modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(p.Modules))
	}
	if p.Merged != p.Modules[0].Model {
		t.Fatal("single-module merge must reuse the module's own compiled model (identity)")
	}
}

// TestDirectoryAutoDiscoverHashMatches loads the credit example as a DIRECTORY (no manifest ⇒
// auto-discover the single credit.rules). The hash must still match the standalone compile.
func TestDirectoryAutoDiscoverHashMatches(t *testing.T) {
	src, err := os.ReadFile(creditPath)
	if err != nil {
		t.Fatal(err)
	}
	_, wantHash, _, err := loader.Compile(src)
	if err != nil {
		t.Fatal(err)
	}

	p, err := Load(filepath.Dir(creditPath))
	if err != nil {
		t.Fatalf("Load(dir): %v", err)
	}
	if p.Hash != wantHash {
		t.Fatalf("auto-discovered project hash %s != loader hash %s", p.Hash, wantHash)
	}
	if got, want := p.Manifest.Name, "credit"; got != want {
		t.Errorf("project name = %q, want %q (dir base)", got, want)
	}
	if _, ok := p.Module("credit"); !ok {
		t.Errorf("expected a module named %q", "credit")
	}
}

func TestValidateModuleName(t *testing.T) {
	bad := []string{"", "a__b", "a.b", "a/b", "a b"}
	for _, n := range bad {
		if err := validateModuleName(n); err == nil {
			t.Errorf("validateModuleName(%q) = nil, want error", n)
		}
	}
	for _, n := range []string{"credit", "income_tax", "moduleA"} {
		if err := validateModuleName(n); err != nil {
			t.Errorf("validateModuleName(%q) = %v, want nil", n, err)
		}
	}
}
