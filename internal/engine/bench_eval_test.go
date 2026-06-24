package engine_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

const benchTable = `model "promo" {}
input cart_total : number >= 0
input is_member  : boolean
input promo_code : string
decision discount_pct : number {
  needs: cart_total, is_member, promo_code
  hit: collect max
  >= 50  | -    | -        => 5
  >= 100 | -    | -        => 10
  -      | true | -        => 8
  -      | -    | "BIG20"  => 20
}`

const benchExpr = `model "calc" {}
input principal : number >= 0
input rate      : number
input months    : number
bkm monthly(p:number, r:number):number = p * (r / 12)
decision payment : number = round(monthly(principal, rate) + abs(principal - 1000) / months, 2)`

func compileBench(b *testing.B, src string) *ir.CompiledModel {
	b.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		b.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		b.Fatal(err)
	}
	return cm
}

// Compile-once, evaluate-many: the hot path for a served model / reactive UI.
func BenchmarkEvalTable(b *testing.B) {
	cm := compileBench(b, benchTable)
	in := map[string]any{"cart_total": 120, "is_member": true, "promo_code": "BIG20"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Eval(cm, "discount_pct", in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkEvalExpr(b *testing.B) {
	cm := compileBench(b, benchExpr)
	in := map[string]any{"principal": 250000, "rate": 0.05, "months": 360}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Eval(cm, "payment", in); err != nil {
			b.Fatal(err)
		}
	}
}

// One-shot: parse + compile + evaluate (cold path).
func BenchmarkRunTable(b *testing.B) {
	in := map[string]any{"cart_total": 120, "is_member": true, "promo_code": "BIG20"}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Run(benchTable, "discount_pct", in); err != nil {
			b.Fatal(err)
		}
	}
}
