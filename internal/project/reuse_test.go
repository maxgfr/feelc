package project

import (
	"fmt"
	"testing"
)

// reuseMod builds a valid one-decision module; changing `mid` changes the source bytes (and the
// compiled model), so it models an "edited" module versus an "unchanged" one.
func reuseMod(name string, mid int) SourceModule {
	src := fmt.Sprintf("model %q {\n  rounding: half_even\n}\ninput x : number in [0..100]\n\ndecision tier : string {\n  needs: x\n  hit: first\n  >= 80 => \"hi\"\n  >= %d => \"mid\"\n  default => \"lo\"\n}\n", name, mid)
	return SourceModule{Name: name, Source: src}
}

// TestCompileReusingReusesUnchangedModules is the acceptance gate for the incremental candidate-verify
// fast path: CompileReusing must reuse the compiled model AND verify report (same pointers) of a module
// whose source is unchanged versus base, and recompile only the changed one. That identity is what keeps
// POST /v1/project/verify O(changed) instead of O(N) when an AI edits one module among many.
func TestCompileReusingReusesUnchangedModules(t *testing.T) {
	base, err := Compile("p", []SourceModule{reuseMod("a", 10), reuseMod("b", 20)})
	if err != nil {
		t.Fatalf("Compile base: %v", err)
	}

	// Recompile with "b" edited and "a" byte-identical.
	cand, err := CompileReusing("p", []SourceModule{reuseMod("a", 10), reuseMod("b", 30)}, base)
	if err != nil {
		t.Fatalf("CompileReusing: %v", err)
	}

	ba, _ := base.Module("a")
	ca, _ := cand.Module("a")
	if ca.Model != ba.Model || ca.Report != ba.Report {
		t.Error(`unchanged module "a" must reuse base's compiled model and verify report (no recompile)`)
	}

	bb, _ := base.Module("b")
	cb, _ := cand.Module("b")
	if cb.Model == bb.Model {
		t.Error(`edited module "b" must be recompiled (fresh model), not reused`)
	}

	// A nil base disables reuse and still links (parity with Compile).
	fresh, err := CompileReusing("p", []SourceModule{reuseMod("a", 10)}, nil)
	if err != nil || len(fresh.Modules) != 1 {
		t.Fatalf("CompileReusing(nil base): err=%v modules=%d", err, len(fresh.Modules))
	}
}
