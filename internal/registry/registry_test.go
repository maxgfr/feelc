package registry_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/registry"
)

// Le snapshot pris par une requête survit à un swap concurrent (atomic.Pointer), et le rollback
// republie le modèle précédent.
func TestStoreSnapshotAndRollback(t *testing.T) {
	reg := registry.New()
	if reg.Current() != nil {
		t.Fatal("registre vide attendu au départ")
	}
	e1 := reg.Store(&ir.CompiledModel{Name: "a"}, "ha")
	snap := reg.Current() // requête en vol capture ce snapshot
	e2 := reg.Store(&ir.CompiledModel{Name: "b"}, "hb")

	if e1.Version != 1 || e2.Version != 2 {
		t.Fatalf("versions = %d, %d ; attendu 1, 2", e1.Version, e2.Version)
	}
	if snap.Model.Name != "a" {
		t.Errorf("le snapshot doit rester sur le modèle 'a' malgré le swap, obtenu %q", snap.Model.Name)
	}
	if reg.Current().Model.Name != "b" {
		t.Errorf("courant doit être 'b', obtenu %q", reg.Current().Model.Name)
	}

	r, ok := reg.Rollback()
	if !ok || r.Model.Name != "a" {
		t.Fatalf("rollback vers 'a' attendu, obtenu ok=%v name=%v", ok, r)
	}
	if reg.Current().Model.Name != "a" {
		t.Errorf("après rollback, courant doit être 'a'")
	}
}
