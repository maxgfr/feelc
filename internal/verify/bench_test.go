package verify_test

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/verify"
)

// buildSubsumeModel construit DIRECTEMENT un IR (sans DSL) : nRules règles `>= k` sur nCols
// colonnes, bornes tirées d'un petit pool (4 valeurs) -> grille bornée < gridBudget, et
// nombreux chevauchements/redondances (sortie identique) pour exercer la matrice de subsumption.
func buildSubsumeModel(nRules, nCols int) *ir.CompiledModel {
	tbl := &ir.DecisionTable{HitPolicy: ir.HitAny, Outputs: []string{"out"}}
	for j := 0; j < nCols; j++ {
		tbl.Inputs = append(tbl.Inputs, fmt.Sprintf("c%d", j))
	}
	for i := 0; i < nRules; i++ {
		r := ir.Rule{Outputs: []ir.Value{ir.Str("x")}} // sortie identique partout -> redondances, pas de conflit
		for j := 0; j < nCols; j++ {
			lo := int64((i + j) % 4)
			r.Conds = append(r.Conds, ir.CellTest{Op: ir.OpGe, A: ir.Num(decimal.FromInt(lo))})
		}
		tbl.Rules = append(tbl.Rules, r)
	}
	cm := &ir.CompiledModel{Name: "bench", Inputs: map[string]ir.Type{}, Domains: map[string]ir.Domain{}}
	for j := 0; j < nCols; j++ {
		name := fmt.Sprintf("c%d", j)
		cm.Inputs[name] = ir.TypeNumber
		cm.Domains[name] = ir.Domain{Kind: ir.DomNumeric, Lo: ir.Num(decimal.FromInt(0)), Hi: ir.Num(decimal.FromInt(10))}
	}
	cm.Decisions = []ir.Decision{{Name: "d", Kind: ir.KindTable, Table: tbl, Deps: tbl.Inputs}}
	return cm
}

func BenchmarkVerifySubsumption(b *testing.B) {
	cm := buildSubsumeModel(50, 5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = verify.Verify(cm)
	}
}

// Garde-fou perf : verify d'une table 50×5 doit rester rapide. La matrice de subsumption est
// O(points × règles_couvrantes) (bitset) ; une régression algorithmique (ex: O(points × règles²)
// dans la boucle chaude) la ferait exploser. Médiane de 3 (anti-bruit), budget généreux,
// ignorable en -short, surchargé par FEELC_VERIFY_PERF_BUDGET_MS — échoue franchement, jamais flaky.
func TestVerifySubsumptionPerfGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("garde-fou perf ignoré en -short")
	}
	cm := buildSubsumeModel(50, 5)

	// La table doit être réellement vérifiée (grille sous budget), sinon le garde-fou est vide.
	rep := verify.Verify(cm)
	if has(rep, verify.KindNotVerifiable) != nil {
		t.Fatalf("la table de bench ne doit pas dégrader en non-vérifiable (grille trop grande ?)")
	}
	if has(rep, verify.KindSubsumed) == nil {
		t.Fatalf("la table de bench doit produire des findings de subsumption")
	}

	var durs []time.Duration
	for i := 0; i < 3; i++ {
		start := time.Now()
		_ = verify.Verify(cm)
		durs = append(durs, time.Since(start))
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	median := durs[1]

	budget := 5 * time.Second
	if v := os.Getenv("FEELC_VERIFY_PERF_BUDGET_MS"); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			budget = time.Duration(ms) * time.Millisecond
		}
	}
	if median > budget {
		t.Fatalf("verify 50×5 médiane %v > budget %v — régression algorithmique de subsumption ?", median, budget)
	}
	t.Logf("verify 50×5 médiane %v (budget %v)", median, budget)
}
