package engine_test

import (
	"strings"
	"testing"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/engine"
)

func runStr(t *testing.T, src, dec string, in map[string]any) (any, error) {
	t.Helper()
	return engine.Run(src, dec, in)
}

func TestHitUnique(t *testing.T) {
	src := `model "m" {}
input n : number
decision grade : string {
  needs: n
  hit: unique
     [0..10)  => "A"
     [10..20) => "B"
}`
	if got, _ := runStr(t, src, "grade", map[string]any{"n": 5}); got != "A" {
		t.Errorf("grade(5) = %v, attendu A", got)
	}
	if got, _ := runStr(t, src, "grade", map[string]any{"n": 15}); got != "B" {
		t.Errorf("grade(15) = %v, attendu B", got)
	}

	conflict := `model "m" {}
input n : number
decision bad : string {
  needs: n
  hit: unique
     >= 0 => "x"
     >= 5 => "y"
}`
	if _, err := runStr(t, conflict, "bad", map[string]any{"n": 10}); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("attendu erreur UNIQUE pour 2 matchs, obtenu %v", err)
	}
}

func TestHitAny(t *testing.T) {
	ok := `model "m" {}
input n : number
decision a : string {
  needs: n
  hit: any
     >= 0  => "ok"
     >= 10 => "ok"
}`
	if got, err := runStr(t, ok, "a", map[string]any{"n": 15}); err != nil || got != "ok" {
		t.Errorf("any concordant: got %v err %v", got, err)
	}
	conflict := `model "m" {}
input n : number
decision a : string {
  needs: n
  hit: any
     >= 0  => "x"
     >= 10 => "y"
}`
	if _, err := runStr(t, conflict, "a", map[string]any{"n": 15}); err == nil || !strings.Contains(err.Error(), "ANY") {
		t.Errorf("attendu erreur ANY (conflit), obtenu %v", err)
	}
}

func TestHitCollectAggregations(t *testing.T) {
	mk := func(hit string) string {
		return `model "m" {}
input amount : number
decision r : number {
  needs: amount
  hit: ` + hit + `
     >= 100  => 10
     >= 500  => 20
     >= 1000 => 30
}`
	}
	num := func(t *testing.T, hit string, amount int) string {
		t.Helper()
		out, err := runStr(t, mk(hit), "r", map[string]any{"amount": amount})
		if err != nil {
			t.Fatalf("%s amount=%d: %v", hit, amount, err)
		}
		d, ok := out.(*apd.Decimal)
		if !ok {
			t.Fatalf("%s: attendu décimal, obtenu %T", hit, out)
		}
		return d.Text('f')
	}
	if got := num(t, "collect sum", 1200); got != "60" { // 10+20+30
		t.Errorf("collect sum(1200) = %s, attendu 60", got)
	}
	if got := num(t, "collect sum", 50); got != "0" { // aucun match
		t.Errorf("collect sum(50) = %s, attendu 0", got)
	}
	if got := num(t, "collect count", 1200); got != "3" {
		t.Errorf("collect count(1200) = %s, attendu 3", got)
	}
	if got := num(t, "collect max", 1200); got != "30" {
		t.Errorf("collect max(1200) = %s, attendu 30", got)
	}
	if got := num(t, "collect min", 1200); got != "10" {
		t.Errorf("collect min(1200) = %s, attendu 10", got)
	}
}

func TestHitCollectList(t *testing.T) {
	src := `model "m" {}
input amount : number
decision r : number {
  needs: amount
  hit: collect
     >= 100  => 10
     >= 500  => 20
     >= 1000 => 30
}`
	out, err := runStr(t, src, "r", map[string]any{"amount": 1200})
	if err != nil {
		t.Fatal(err)
	}
	xs, ok := out.([]any)
	if !ok {
		t.Fatalf("attendu liste, obtenu %T", out)
	}
	if len(xs) != 3 {
		t.Errorf("liste de %d éléments, attendu 3", len(xs))
	}
}

func TestHitPriority(t *testing.T) {
	src := `model "m" {}
input score : number
decision verdict : string {
  needs: score
  hit: priority
  priority: "reject", "review", "approve"
     >= 0   => "approve"
     >= 700 => "review"
     < 600  => "reject"
}`
	for _, c := range []struct {
		score int
		want  string
	}{
		{500, "reject"},  // matche approve(>=0) ET reject(<600) -> reject (plus prioritaire)
		{800, "review"},  // matche approve(>=0) ET review(>=700) -> review
		{650, "approve"}, // matche approve seulement
	} {
		got, err := runStr(t, src, "verdict", map[string]any{"score": c.score})
		if err != nil {
			t.Fatalf("score=%d: %v", c.score, err)
		}
		if got != c.want {
			t.Errorf("verdict(%d) = %v, attendu %q", c.score, got, c.want)
		}
	}
}
