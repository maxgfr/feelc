package engine_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// BKM scalaire invoqué dans une décision literal-expression. L'invocation est inlinée à la
// compilation (substitution AST des paramètres), zéro frame d'appel au runtime.
func TestBKMScalarInLiteralExpr(t *testing.T) {
	src := `model "m" {}
input monthly_debt : number
input annual_income : number
bkm dti(debt:number, income:number):number = debt / (income / 12)
decision verdict : boolean = dti(monthly_debt, annual_income) <= 0.36`

	got, err := engine.Run(src, "verdict", map[string]any{"monthly_debt": 1500, "annual_income": 60000})
	if err != nil {
		t.Fatalf("compile/run: %v", err)
	}
	if got != true {
		t.Errorf("verdict(dti=0.3) = %v, attendu true", got)
	}
	got, err = engine.Run(src, "verdict", map[string]any{"monthly_debt": 2000, "annual_income": 60000})
	if err != nil {
		t.Fatal(err)
	}
	if got != false {
		t.Errorf("verdict(dti=0.4) = %v, attendu false", got)
	}
}

// BKM invoqué dans une cellule de table (Op=Prog).
func TestBKMInTableCell(t *testing.T) {
	src := `model "m" {}
input monthly_debt : number
decision flag : boolean {
  needs: monthly_debt
  hit: first
  dti(monthly_debt, 60000) <= 0.36 => true
  -                                 => false
}
bkm dti(debt:number, income:number):number = debt / (income / 12)`

	got, err := engine.Run(src, "flag", map[string]any{"monthly_debt": 1500})
	if err != nil {
		t.Fatalf("compile/run: %v", err)
	}
	if got != true {
		t.Errorf("flag(1500) = %v, attendu true", got)
	}
	got, _ = engine.Run(src, "flag", map[string]any{"monthly_debt": 2000})
	if got != false {
		t.Errorf("flag(2000) = %v, attendu false", got)
	}
}

// Invocations imbriquées f(g(x)) : inlining récursif borné, non cyclique.
func TestBKMNested(t *testing.T) {
	src := `model "m" {}
input n : number
bkm half(x:number):number = x / 2
bkm quarter(x:number):number = half(half(x))
decision q : number = quarter(n)`

	got, err := engine.Run(src, "q", map[string]any{"n": 100})
	if err != nil {
		t.Fatalf("compile/run: %v", err)
	}
	if got == nil {
		t.Fatal("résultat nil")
	}
	if s, ok := got.(interface{ Text(byte) string }); ok {
		if v := s.Text('f'); v != "25" {
			t.Errorf("quarter(100) = %s, attendu 25", v)
		}
	} else {
		t.Fatalf("type de sortie inattendu: %T", got)
	}
}

// Échecs FRANCS (jamais conformer en silence) : arité, BKM inconnu, récursion, kwargs, `?`.
func TestBKMHardFailures(t *testing.T) {
	base := `model "m" {}
input n : number
`
	cases := []struct {
		name    string
		extra   string
		wantSub string
	}{
		{"arité incorrecte",
			"bkm f(a:number, b:number):number = a + b\ndecision d : number = f(n)", "argument"},
		{"BKM inconnu",
			"decision d : number = nope(n)", "inconnu"},
		{"récursion BKM",
			"bkm loop(x:number):number = loop(x)\ndecision d : number = loop(n)", "récursion"},
		{"arguments nommés (kwargs)",
			"bkm f(a:number, b:number):number = a + b\ndecision d : number = f(a: n, b: 1)", "nommé"},
		{"? dans un corps de BKM",
			"bkm bad(x:number):number = ? + x\ndecision d : number = bad(n)", "interdit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := engine.Run(base+c.extra, "d", map[string]any{"n": 1})
			if err == nil {
				t.Fatalf("erreur attendue contenant %q, obtenu nil", c.wantSub)
			}
			if !strings.Contains(err.Error(), c.wantSub) {
				t.Errorf("erreur = %q, attendu contenir %q", err.Error(), c.wantSub)
			}
		})
	}
}
