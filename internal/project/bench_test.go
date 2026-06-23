package project

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// genProject writes a temp project of n independent modules and returns its directory.
func genProject(tb testing.TB, n int) string {
	tb.Helper()
	dir := tb.TempDir()
	refs := make([]ModuleRef, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("m%d", i)
		src := fmt.Sprintf("model %q {\n  rounding: half_even\n}\ninput x : number in [0..100]\n\ndecision tier : string {\n  needs: x\n  hit: first\n  >= 80 => \"hi\"\n  >= %d => \"mid\"\n  default => \"lo\"\n}\n", name, (i%50)+1)
		if err := os.WriteFile(filepath.Join(dir, name+".rules"), []byte(src), 0o644); err != nil {
			tb.Fatal(err)
		}
		refs[i] = ModuleRef{Name: name, Path: name + ".rules"}
	}
	man, _ := json.Marshal(Manifest{Name: "bench", Modules: refs})
	if err := os.WriteFile(filepath.Join(dir, ManifestName), man, 0o644); err != nil {
		tb.Fatal(err)
	}
	return dir
}

// BenchmarkProjectLoad measures a full (parallel) compile+verify+link of an N-module project.
func BenchmarkProjectLoad(b *testing.B) {
	for _, n := range []int{20, 100} {
		dir := genProject(b, n)
		b.Run(fmt.Sprintf("modules=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				if _, err := Load(dir); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkWorkspaceEdit measures editing ONE module in an N-module project (validate + persist +
// incremental reload). The per-edit cost should stay roughly flat as N grows — only the edited module
// recompiles, the rest are reused from the cache.
func BenchmarkWorkspaceEdit(b *testing.B) {
	for _, n := range []int{20, 100} {
		dir := genProject(b, n)
		ws, err := OpenWorkspace(dir, false)
		if err != nil {
			b.Fatal(err)
		}
		b.Run(fmt.Sprintf("modules=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				src := fmt.Sprintf("model \"m0\" {\n  rounding: half_even\n}\n# rev %d\ninput x : number in [0..100]\n\ndecision tier : string {\n  needs: x\n  hit: first\n  >= 80 => \"hi\"\n  >= 10 => \"mid\"\n  default => \"lo\"\n}\n", i)
				if _, err := ws.PutModule("m0", src); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
