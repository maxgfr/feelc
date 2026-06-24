package project

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPutModuleIfMatch covers the optimistic-concurrency guard: a stale If-Match is rejected with
// ErrPreconditionFailed WITHOUT touching disk, the current hash succeeds (and the hash advances), and an
// empty If-Match keeps the last-writer-wins back-compat.
func TestPutModuleIfMatch(t *testing.T) {
	dir := writeTempProject(t)
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := ws.Current().Module("a")
	if !ok {
		t.Fatal("module a missing")
	}
	cur := m.Hash
	edit := "model \"a\" {}\ninput age : number in [0..120]\ndecision adult : boolean = age >= 21\n"

	// Stale If-Match → ErrPreconditionFailed, on-disk file unchanged (golden rule still holds).
	if _, err := ws.PutModuleIfMatch("a", edit, "deadbeef"); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("stale If-Match: got %v, want ErrPreconditionFailed", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.rules")); strings.Contains(string(b), ">= 21") {
		t.Error("a stale If-Match must not write to disk")
	}

	// Matching If-Match → success, hash advances.
	p, err := ws.PutModuleIfMatch("a", edit, cur)
	if err != nil {
		t.Fatalf("matching If-Match: %v", err)
	}
	nm, _ := p.Module("a")
	if nm.Hash == cur {
		t.Error("module hash should change after a successful edit")
	}

	// Empty If-Match → last-writer-wins (back-compat).
	again := "model \"a\" {}\ninput age : number in [0..120]\ndecision adult : boolean = age >= 30\n"
	if _, err := ws.PutModuleIfMatch("a", again, ""); err != nil {
		t.Fatalf("empty If-Match should succeed: %v", err)
	}
}

// TestCreateModuleIfNoneMatch covers If-None-Match:* create semantics: an existing name conflicts with
// ErrPreconditionFailed, a fresh name succeeds.
func TestCreateModuleIfNoneMatch(t *testing.T) {
	dir := writeTempProject(t)
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	src := "model \"a\" {}\ninput x : boolean\ndecision y : boolean = x\n"
	if _, err := ws.CreateModuleIfNoneMatch("a", src, true); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("create existing w/ If-None-Match:*: got %v, want ErrPreconditionFailed", err)
	}
	if _, err := ws.CreateModuleIfNoneMatch("c", src, true); err != nil {
		t.Fatalf("create new module: %v", err)
	}
}

// TestDeleteModuleIfMatch covers the optimistic-concurrency guard on delete.
func TestDeleteModuleIfMatch(t *testing.T) {
	dir := writeTempProject(t)
	ws, err := OpenWorkspace(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	m, _ := ws.Current().Module("b")
	// Stale If-Match → rejected, module still present.
	if _, err := ws.DeleteModuleIfMatch("b", "deadbeef"); !errors.Is(err, ErrPreconditionFailed) {
		t.Fatalf("stale If-Match delete: got %v, want ErrPreconditionFailed", err)
	}
	if _, ok := ws.Current().Module("b"); !ok {
		t.Error("module b must survive a stale-If-Match delete")
	}
	// Matching If-Match → deleted.
	p, err := ws.DeleteModuleIfMatch("b", m.Hash)
	if err != nil {
		t.Fatalf("matching If-Match delete: %v", err)
	}
	if _, ok := p.Module("b"); ok {
		t.Error("module b should be gone after a matching-If-Match delete")
	}
}
