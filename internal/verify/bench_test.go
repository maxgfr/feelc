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

// buildSubsumeModel builds an IR DIRECTLY (without DSL): nRules rules `>= k` over nCols
// columns, bounds drawn from a small pool (4 values) -> bounded grid < gridBudget, and
// many overlaps/redundancies (identical output) to exercise the subsumption matrix.
func buildSubsumeModel(nRules, nCols int) *ir.CompiledModel {
	tbl := &ir.DecisionTable{HitPolicy: ir.HitAny, Outputs: []string{"out"}}
	for j := 0; j < nCols; j++ {
		tbl.Inputs = append(tbl.Inputs, fmt.Sprintf("c%d", j))
	}
	for i := 0; i < nRules; i++ {
		r := ir.Rule{Outputs: []ir.Value{ir.Str("x")}} // identical output everywhere -> redundancies, no conflict
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

// Perf guard: verify of a 50×5 table must stay fast. The subsumption matrix is
// O(points × covering_rules) (bitset); an algorithmic regression (e.g. O(points × rules²)
// in the hot loop) would make it explode. Median of 3 (anti-noise), generous budget,
// skippable in -short, overridden by FEELC_VERIFY_PERF_BUDGET_MS — fails cleanly, never flaky.
func TestVerifySubsumptionPerfGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("perf guard skipped in -short")
	}
	cm := buildSubsumeModel(50, 5)

	// The table must actually be verified (grid under budget), otherwise the guard is empty.
	rep := verify.Verify(cm)
	if has(rep, verify.KindNotVerifiable) != nil {
		t.Fatalf("the bench table must not degrade to non-verifiable (grid too large?)")
	}
	if has(rep, verify.KindSubsumed) == nil {
		t.Fatalf("the bench table must produce subsumption findings")
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
		t.Fatalf("verify 50×5 median %v > budget %v — subsumption algorithmic regression?", median, budget)
	}
	t.Logf("verify 50×5 median %v (budget %v)", median, budget)
}
