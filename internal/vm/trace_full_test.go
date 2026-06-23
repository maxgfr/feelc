package vm_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/vm"
)

func mkInputs(t *testing.T, cm *ir.CompiledModel, raw map[string]any) map[string]ir.Value {
	t.Helper()
	in := make(map[string]ir.Value, len(raw))
	for k, v := range raw {
		val, err := ir.FromAny(v)
		if err != nil {
			t.Fatal(err)
		}
		in[k] = val
	}
	if err := ir.CoerceInputs(cm, in); err != nil {
		t.Fatal(err)
	}
	return in
}

// TraceFull walks the whole DRG path of a goal: every upstream decision is traced in
// dependency-first order (goal last) on a single shared evaluator, and @source citations
// along the path are collected.
func TestTraceFull(t *testing.T) {
	m, err := dsl.Parse(`model "t" {}
input a : number >= 0
input b : number >= 0
@source "doc A"
decision ratio : number = a / b
@source "doc B"
decision band : string {
  needs: ratio
  hit: first
  < 1 => "low"
  -   => "high"
}`)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}

	ft, err := vm.TraceFull(cm, "band", mkInputs(t, cm, map[string]any{"a": 1, "b": 4}))
	if err != nil {
		t.Fatal(err)
	}
	if ft.Goal != "band" {
		t.Errorf("Goal = %q, want band", ft.Goal)
	}
	if len(ft.Path) != 2 {
		t.Fatalf("Path len = %d, want 2 (ratio, band)", len(ft.Path))
	}
	if ft.Path[0].Decision != "ratio" || ft.Path[1].Decision != "band" {
		t.Errorf("Path order = [%s,%s], want [ratio,band]", ft.Path[0].Decision, ft.Path[1].Decision)
	}
	if ft.Result != ft.Path[len(ft.Path)-1] {
		t.Error("Result must alias the last path entry")
	}
	if ft.Result.Decision != "band" {
		t.Errorf("Result.Decision = %q, want band", ft.Result.Decision)
	}
	if ft.Path[1].Output != "low" {
		t.Errorf("band output = %v, want low", ft.Path[1].Output)
	}
	srcs := map[string]string{}
	for _, s := range ft.Sources {
		srcs[s.Decision] = s.Source
	}
	if srcs["ratio"] != "doc A" || srcs["band"] != "doc B" {
		t.Errorf("Sources = %+v, want ratio=doc A band=doc B", ft.Sources)
	}

	// unknown goal errors.
	if _, err := vm.TraceFull(cm, "nope", nil); err == nil {
		t.Error("unknown goal must error")
	}
}
