package registry_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/registry"
)

// The snapshot taken by a request survives a concurrent swap (atomic.Pointer), and rollback
// republishes the previous model.
func TestStoreSnapshotAndRollback(t *testing.T) {
	reg := registry.New()
	if reg.Current() != nil {
		t.Fatal("expected empty registry at start")
	}
	e1 := reg.Store(&ir.CompiledModel{Name: "a"}, "ha")
	snap := reg.Current() // in-flight request captures this snapshot
	e2 := reg.Store(&ir.CompiledModel{Name: "b"}, "hb")

	if e1.Version != 1 || e2.Version != 2 {
		t.Fatalf("versions = %d, %d ; expected 1, 2", e1.Version, e2.Version)
	}
	if snap.Model.Name != "a" {
		t.Errorf("the snapshot must stay on model 'a' despite the swap, got %q", snap.Model.Name)
	}
	if reg.Current().Model.Name != "b" {
		t.Errorf("current must be 'b', got %q", reg.Current().Model.Name)
	}

	r, ok := reg.Rollback()
	if !ok || r.Model.Name != "a" {
		t.Fatalf("expected rollback to 'a', got ok=%v name=%v", ok, r)
	}
	if reg.Current().Model.Name != "a" {
		t.Errorf("after rollback, current must be 'a'")
	}
}
