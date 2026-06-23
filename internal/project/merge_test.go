package project

import (
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

// TestMergeIsolatesCollidingInputs loads two modules that both declare `age` and confirms the merge
// keeps them isolated under distinct qualified names, and that each decision evaluates against its own.
func TestMergeIsolatesCollidingInputs(t *testing.T) {
	p, err := Load("testdata/multi")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Modules) != 2 {
		t.Fatalf("modules = %d, want 2", len(p.Modules))
	}

	// Both `age` inputs survive, namespaced and distinct.
	for _, want := range []string{"alpha__age", "beta__age"} {
		if _, ok := p.Merged.Inputs[want]; !ok {
			t.Errorf("merged model missing input %q", want)
		}
	}
	if _, ok := p.Merged.Inputs["age"]; ok {
		t.Error("merged model leaked an unqualified `age` input")
	}

	// Decisions are namespaced and resolve their own module's input.
	got, err := engine.Eval(p.Merged, "alpha__adult", map[string]any{"alpha__age": 20})
	if err != nil {
		t.Fatalf("eval alpha__adult: %v", err)
	}
	if got != true {
		t.Errorf("alpha__adult(age=20) = %v, want true", got)
	}
	got, err = engine.Eval(p.Merged, "beta__senior", map[string]any{"beta__age": 20})
	if err != nil {
		t.Fatalf("eval beta__senior: %v", err)
	}
	if got != false {
		t.Errorf("beta__senior(age=20) = %v, want false", got)
	}

	// Cross-talk is impossible: alpha__adult only reads alpha__age, never beta__age.
	got, err = engine.Eval(p.Merged, "alpha__adult", map[string]any{"alpha__age": 70, "beta__age": 10})
	if err != nil {
		t.Fatalf("eval alpha__adult (both ages): %v", err)
	}
	if got != true {
		t.Errorf("alpha__adult should read only alpha__age; got %v", got)
	}
}

// TestMergeHashOrderIndependent confirms the project hash does not depend on the manifest's module
// ordering (Load sorts modules by name before merging).
func TestMergeHashOrderIndependent(t *testing.T) {
	a, err := Load("testdata/multi")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Load("testdata/multi-reordered")
	if err != nil {
		t.Fatal(err)
	}
	if a.Hash != b.Hash {
		t.Fatalf("hash depends on manifest order: %s != %s", a.Hash, b.Hash)
	}
	// And the order of p.Modules is normalized (sorted) regardless of manifest.
	if a.Modules[0].Name != "alpha" || a.Modules[1].Name != "beta" {
		t.Errorf("modules not name-sorted: %s, %s", a.Modules[0].Name, a.Modules[1].Name)
	}
}

// TestMergeDoesNotMutateStandaloneModels guards the deep-copy: qualifying for the merge must not
// rewrite the per-module standalone Model (kept for per-module hashing/verify).
func TestMergeDoesNotMutateStandaloneModels(t *testing.T) {
	p, err := Load("testdata/multi")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range p.Modules {
		// The standalone model still uses the bare local name `age`, never the qualified one.
		if _, ok := m.Model.Inputs["age"]; !ok {
			t.Errorf("module %q standalone model lost its bare `age` input (mutated by merge?)", m.Name)
		}
		for _, d := range m.Model.Decisions {
			for _, dep := range d.Deps {
				if dep == m.Name+sep+"age" {
					t.Errorf("module %q standalone decision %q had a dep qualified in place: %q", m.Name, d.Name, dep)
				}
			}
		}
		// Recomputing the standalone hash must still match the cached per-module hash.
		h, err := ir.Hash(m.Model)
		if err != nil {
			t.Fatal(err)
		}
		if got := hexHash(h); got != m.Hash {
			t.Errorf("module %q standalone hash changed: %s != %s", m.Name, got, m.Hash)
		}
	}
}

func hexHash(h [32]byte) string {
	const hexdigits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, b := range h {
		out[i*2] = hexdigits[b>>4]
		out[i*2+1] = hexdigits[b&0x0f]
	}
	return string(out)
}
