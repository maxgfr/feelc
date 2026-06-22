package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/verify"
)

// not(<test>) reste GÉOMÉTRIQUE : le vérificateur l'analyse (pas de dégradation Op=Prog).
// Une table `"urban"` + `not("urban")` couvre tout le domaine -> aucun trou bloquant.
func TestNotCellStaysVerifiable(t *testing.T) {
	src := `model "m" {}
input region : string in {"urban", "suburban", "rural"}
decision z : string {
  needs: region
  hit: first
  "urban"      => "u"
  not("urban") => "other"
}`
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	rep := verify.Verify(cm)
	for _, f := range rep.Findings {
		if f.Kind == verify.KindNotVerifiable {
			t.Errorf("not() ne doit PAS dégrader en non-vérifiable: %+v", f)
		}
		if f.Severity == verify.SevError {
			t.Errorf("table complète via not() : aucun bloqueur attendu, obtenu %+v", f)
		}
	}
}

// jn passe un nombre d'entrée EXACT (json.Number) — pas de float64 lossy.
func jn(s string) any { return json.Number(s) }

// if/then/else en décision literal-expression (backpatch OpJmpFalse/OpJmp).
func TestIfThenElse(t *testing.T) {
	src := `model "m" {}
input score : number
decision tier : number = if score >= 700 then 1 else 0`
	if got, err := engine.Run(src, "tier", map[string]any{"score": jn("750")}); err != nil || numText(t, got) != "1" {
		t.Fatalf("if(score=750) = %v (err %v), attendu 1", got, err)
	}
	if got, err := engine.Run(src, "tier", map[string]any{"score": jn("600")}); err != nil || numText(t, got) != "0" {
		t.Fatalf("if(score=600) = %v (err %v), attendu 0", got, err)
	}
}

// Built-ins mono-arg floor / ceiling / round (round = HALF_EVEN, déterministe).
func TestMonoArgBuiltins(t *testing.T) {
	src := `model "m" {}
input x : number
decision fl : number = floor(x)
decision ce : number = ceiling(x)
decision rd : number = round(x)`
	cases := []struct {
		dec  string
		x    string
		want string
	}{
		{"fl", "2.7", "2"}, {"fl", "-2.1", "-3"},
		{"ce", "2.1", "3"}, {"ce", "-2.7", "-2"},
		{"rd", "2.5", "2"}, {"rd", "3.5", "4"}, {"rd", "2.4", "2"}, {"rd", "2.6", "3"},
	}
	for _, c := range cases {
		got, err := engine.Run(src, c.dec, map[string]any{"x": jn(c.x)})
		if err != nil {
			t.Fatalf("%s(%s): %v", c.dec, c.x, err)
		}
		if numText(t, got) != c.want {
			t.Errorf("%s(%s) = %s, attendu %s", c.dec, c.x, numText(t, got), c.want)
		}
	}
}

// not(<test>) en cellule : négation géométrique (équivalence valeur, intervalle, comparaison),
// + multi-tests not(a, b) = hors ensemble.
func TestNotCellNegation(t *testing.T) {
	src := `model "m" {}
input region : string in {"urban", "suburban", "rural"}
input n : number in [0..100]
decision zone : string {
  needs: region
  hit: first
  not("urban") => "other"
  -            => "urban_zone"
}
decision band : string {
  needs: n
  hit: first
  not([0..10]) => "out"
  -            => "in"
}
decision cmp : string {
  needs: n
  hit: first
  not(? < 5) => "ge5"
  -          => "lt5"
}
decision set : string {
  needs: n
  hit: first
  not(1, 2, 3) => "other"
  -            => "small"
}`
	check := func(dec string, in map[string]any, want string) {
		got, err := engine.Run(src, dec, in)
		if err != nil {
			t.Fatalf("%s%v: %v", dec, in, err)
		}
		if got != want {
			t.Errorf("%s%v = %v, attendu %q", dec, in, got, want)
		}
	}
	check("zone", map[string]any{"region": "rural"}, "other")
	check("zone", map[string]any{"region": "urban"}, "urban_zone")
	check("band", map[string]any{"n": jn("50")}, "out")
	check("band", map[string]any{"n": jn("5")}, "in")
	check("cmp", map[string]any{"n": jn("10")}, "ge5")
	check("cmp", map[string]any{"n": jn("3")}, "lt5")
	check("set", map[string]any{"n": jn("7")}, "other")
	check("set", map[string]any{"n": jn("2")}, "small")
}

// Échecs FRANCS : multi-arg built-in, et `?` hors cellule (literal-expr, direct ou via BKM).
func TestFeelExtHardFailures(t *testing.T) {
	cases := []struct {
		name, src, wantSub string
	}{
		{"round multi-arg",
			"model \"m\" {}\ninput x : number\ndecision d : number = round(x, 1)", "argument"},
		{"? dans literal-expr direct",
			"model \"m\" {}\ninput n : number\ndecision d : number = ? + n", "cellule"},
		{"? via argument de BKM dans literal-expr",
			"model \"m\" {}\ninput n : number\nbkm f(y:number):number = y * 2\ndecision d : number = f(?) + n", "cellule"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := engine.Run(c.src, "d", map[string]any{"n": jn("1"), "x": jn("1")})
			if err == nil {
				t.Fatalf("erreur de compilation attendue (%q)", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("erreur = %q, attendu contenir %q", err.Error(), c.wantSub)
			}
		})
	}
}
