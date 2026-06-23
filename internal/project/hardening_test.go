package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateModulePathRejectsTraversal(t *testing.T) {
	dir := "/projects/demo"
	bad := []string{"../escape.rules", "../../etc/passwd", "/etc/passwd", "sub/../../escape.rules", ""}
	for _, p := range bad {
		if err := validateModulePath(dir, p); err == nil {
			t.Errorf("validateModulePath(%q) = nil, want error", p)
		}
	}
	for _, p := range []string{"a.rules", "sub/a.rules"} {
		if err := validateModulePath(dir, p); err != nil {
			t.Errorf("validateModulePath(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateModuleNameRejectsControlChars(t *testing.T) {
	for _, n := range []string{"a\nb", "a\rb", "a\tb", "a b", "a\x00b", "1abc", "-abc", "a__b", strings.Repeat("a", 65)} {
		if err := validateModuleName(n); err == nil {
			t.Errorf("validateModuleName(%q) = nil, want error", n)
		}
	}
	for _, n := range []string{"credit", "income_tax", "m1"} {
		if err := validateModuleName(n); err != nil {
			t.Errorf("validateModuleName(%q) = %v, want nil", n, err)
		}
	}
}

func TestManifestPathTraversalRejectedAtLoad(t *testing.T) {
	dir := t.TempDir()
	// A manifest that tries to escape the project directory must be rejected (not silently followed).
	writeFile(t, dir, ManifestName, `{"name":"x","modules":[{"name":"evil","path":"../../../../etc/hosts"}]}`)
	if _, err := Load(dir); err == nil || !strings.Contains(err.Error(), "escape") {
		t.Fatalf("Load with traversal path: got %v, want an 'escapes the project directory' error", err)
	}
}

func TestWorkspaceCreateClobberRejected(t *testing.T) {
	dir := writeTempProject(t)
	// Drop an unrelated file where a new module would be written.
	writeFile(t, dir, "ghost.rules", "model \"ghost\" {}\n")
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ws.CreateModule("ghost", "model \"ghost\" {}\ninput x : boolean\ndecision y : boolean = x\n"); err == nil {
		t.Fatal("CreateModule should refuse to clobber an existing on-disk file")
	}
	// The pre-existing file must be untouched.
	if b, _ := os.ReadFile(filepath.Join(dir, "ghost.rules")); strings.Contains(string(b), "decision y") {
		t.Error("CreateModule clobbered an existing file")
	}
}
