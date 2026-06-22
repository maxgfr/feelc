package engine_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
)

// Table à plusieurs colonnes d'entrée : toutes les conditions d'une ligne doivent matcher (AND).
func TestMultiColumnFirstHit(t *testing.T) {
	src := `
model "loan" {}

input score : number
input age   : number

decision verdict : string {
  needs: score, age
  hit: first
  #  score  | age   => result
     < 580   | -      => "score insuffisant"
     -       | < 18   => "mineur"
     >= 580  | >= 18  => "ok"
}
`
	cases := []struct {
		score, age int
		want       string
	}{
		{500, 40, "score insuffisant"}, // 1re ligne
		{700, 16, "mineur"},            // 2e ligne (score ok mais mineur)
		{700, 40, "ok"},                // 3e ligne
	}
	for _, c := range cases {
		got, err := engine.Run(src, "verdict", map[string]any{"score": c.score, "age": c.age})
		if err != nil {
			t.Fatalf("(%d,%d): %v", c.score, c.age, err)
		}
		if got != c.want {
			t.Errorf("verdict(score=%d,age=%d) = %v, attendu %q", c.score, c.age, got, c.want)
		}
	}
}

// Égalité implicite sur un littéral string (cellule = valeur).
func TestStringEquality(t *testing.T) {
	src := `
model "tier" {}

input plan : string

decision discount : string {
  needs: plan
  hit: first
     "gold"   => "20%"
     "silver" => "10%"
     -        => "0%"
}
`
	for _, c := range []struct{ plan, want string }{
		{"gold", "20%"}, {"silver", "10%"}, {"bronze", "0%"},
	} {
		got, err := engine.Run(src, "discount", map[string]any{"plan": c.plan})
		if err != nil {
			t.Fatalf("plan=%s: %v", c.plan, err)
		}
		if got != c.want {
			t.Errorf("discount(plan=%q) = %v, attendu %q", c.plan, got, c.want)
		}
	}
}

// Discipline de périmètre : un construct hors sous-ensemble v1 doit ÉCHOUER FRANCHEMENT
// (refuser plutôt qu'accepter-puis-mal-interpréter), de même que les erreurs de modèle.
func TestScopeAndErrorDiscipline(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		dec     string
		wantErr string // sous-chaîne attendue
	}{
		{
			name: "needs vers entrée non déclarée",
			src: `model "m" {}
input a : number
decision d : string {
  needs: b
  hit: first
  - => "x"
}`,
			dec:     "d",
			wantErr: "non déclaré",
		},
		{
			name: "hit policy non supportée en v1",
			src: `model "m" {}
input a : number
decision d : string {
  needs: a
  hit: collect
  - => "x"
}`,
			dec:     "d",
			wantErr: "hit policy non supportée",
		},
		{
			name: "décision inconnue à l'évaluation",
			src: `model "m" {}
input a : number
decision d : string {
  needs: a
  hit: first
  - => "x"
}`,
			dec:     "absente",
			wantErr: "décision inconnue",
		},
		{
			name: "contenu après { sur la ligne d'en-tête",
			src: `model "m" {}
input a : number
decision d : string { needs: a
  hit: first
  - => "x"
}`,
			dec:     "d",
			wantErr: "en fin de ligne",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := engine.Run(c.src, c.dec, map[string]any{"a": 1})
			if err == nil {
				t.Fatalf("erreur attendue contenant %q, obtenu nil", c.wantErr)
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("erreur = %q, attendu contenir %q", err.Error(), c.wantErr)
			}
		})
	}
}
