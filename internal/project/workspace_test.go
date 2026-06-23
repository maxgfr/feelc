package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeTempProject creates a writable 2-module project (a, b) with a manifest and returns its dir.
func writeTempProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, ManifestName, `{"name":"tmp","modules":[{"name":"a","path":"a.rules"},{"name":"b","path":"b.rules"}]}`)
	writeFile(t, dir, "a.rules", "model \"a\" {}\ninput age : number in [0..120]\ndecision adult : boolean = age >= 18\n")
	writeFile(t, dir, "b.rules", "model \"b\" {}\ninput age : number in [0..120]\ndecision senior : boolean = age >= 65\n")
	return dir
}

// TestWorkspacePutModuleGoldenRule confirms a valid edit persists + swaps, and an INVALID edit is
// rejected with the on-disk file and current project left untouched.
func TestWorkspacePutModuleGoldenRule(t *testing.T) {
	dir := writeTempProject(t)
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	before := ws.Current().Hash

	// Valid edit → persisted + swapped + hash changes.
	p, err := ws.PutModule("a", "model \"a\" {}\ninput age : number in [0..120]\ndecision adult : boolean = age >= 21\n")
	if err != nil {
		t.Fatalf("valid PutModule: %v", err)
	}
	if p.Hash == before {
		t.Error("hash should change after a real edit")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.rules")); !strings.Contains(string(b), ">= 21") {
		t.Error("edit not persisted to disk")
	}

	// Invalid edit → rejected, project + file unchanged (golden rule).
	good := ws.Current().Hash
	if _, err := ws.PutModule("a", "this is not valid feelc"); err == nil {
		t.Fatal("expected an error for an invalid edit")
	}
	if ws.Current().Hash != good {
		t.Error("golden rule violated: current project changed on an invalid edit")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.rules")); !strings.Contains(string(b), ">= 21") {
		t.Error("golden rule violated: on-disk file overwritten by an invalid edit")
	}
}

// TestWorkspaceCreateDelete covers create (writes file + updates manifest) and delete (removes both).
func TestWorkspaceCreateDelete(t *testing.T) {
	dir := writeTempProject(t)
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	p, err := ws.CreateModule("c", "model \"c\" {}\ninput x : boolean\ndecision y : boolean = x\n")
	if err != nil {
		t.Fatalf("CreateModule: %v", err)
	}
	if _, ok := p.Module("c"); !ok {
		t.Fatal("module c not present after create")
	}
	if mb, _ := os.ReadFile(filepath.Join(dir, ManifestName)); !strings.Contains(string(mb), `"c"`) {
		t.Error("manifest not updated on create")
	}
	if _, err := os.Stat(filepath.Join(dir, "c.rules")); err != nil {
		t.Error("c.rules not written")
	}
	// Duplicate create rejected.
	if _, err := ws.CreateModule("c", "model \"c\" {}\n"); err == nil {
		t.Error("expected duplicate-create rejection")
	}

	p, err = ws.DeleteModule("c")
	if err != nil {
		t.Fatalf("DeleteModule: %v", err)
	}
	if _, ok := p.Module("c"); ok {
		t.Error("module c still present after delete")
	}
	if _, err := os.Stat(filepath.Join(dir, "c.rules")); !os.IsNotExist(err) {
		t.Error("c.rules not removed on delete")
	}
	if mb, _ := os.ReadFile(filepath.Join(dir, ManifestName)); strings.Contains(string(mb), `"c"`) {
		t.Error("manifest still lists c after delete")
	}
}

// TestWorkspaceDeleteRejectedWhenUsed confirms a module bound by another module's `uses` cannot be deleted.
func TestWorkspaceDeleteRejectedWhenUsed(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, ManifestName, `{"name":"lending","modules":[{"name":"kyc","path":"kyc.rules"},{"name":"loan","path":"loan.rules","uses":{"ok":"kyc.passed"}}]}`)
	writeFile(t, dir, "kyc.rules", "model \"kyc\" {}\ninput score : number >= 0\ndecision passed : boolean = score >= 600\n")
	writeFile(t, dir, "loan.rules", "model \"loan\" {}\ninput ok : boolean\ndecision approved : boolean = ok\n")
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ws.DeleteModule("kyc")
	if err == nil || !strings.Contains(err.Error(), "loan") {
		t.Errorf("expected delete rejection naming loan, got %v", err)
	}
}

// TestIncrementalReloadReusesUnchangedModules proves the incremental reload: editing module b must not
// recompile module a (its compiled model is reused from the cache by pointer).
func TestIncrementalReloadReusesUnchangedModules(t *testing.T) {
	dir := writeTempProject(t) // modules a, b
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	aBefore, _ := ws.Current().Module("a")
	aModel := aBefore.Model

	// Edit only module b.
	if _, err := ws.PutModule("b", "model \"b\" {}\ninput age : number in [0..120]\ndecision senior : boolean = age >= 70\n"); err != nil {
		t.Fatal(err)
	}

	aAfter, _ := ws.Current().Module("a")
	if aAfter.Model != aModel {
		t.Error("module a was recompiled on a reload that only changed module b (incremental reuse failed)")
	}
	bAfter, _ := ws.Current().Module("b")
	if bAfter.Model == nil || bAfter.Model == aModel {
		t.Error("module b should have a fresh compiled model after its edit")
	}
}

// TestWorkspaceWatch confirms an on-disk edit hot-reloads the project through the watcher.
func TestWorkspaceWatch(t *testing.T) {
	dir := writeTempProject(t)
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	before := ws.Current().Hash
	got := make(chan *Project, 1)
	stop, err := ws.Watch(func(p *Project, err error) {
		if err == nil {
			select {
			case got <- p:
			default:
			}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer stop()

	writeFile(t, dir, "a.rules", "model \"a\" {}\ninput age : number in [0..120]\ndecision adult : boolean = age >= 30\n")
	select {
	case p := <-got:
		if p.Hash == before {
			t.Error("watch fired but the project hash did not change")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watcher did not fire within 5s")
	}
}
