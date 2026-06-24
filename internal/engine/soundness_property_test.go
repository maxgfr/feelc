package engine_test

import (
	"encoding/json"
	"math/big"
	"math/rand"
	"strconv"
	"testing"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/verify"
)

// TestVerifiedCompleteImpliesNoFallthrough is feelc's central soundness property: if the verifier
// proves a UNIQUE table complete and conflict-free over its declared domain, then a large random
// sweep of in-domain inputs must NEVER fall through to null AND never raise a UNIQUE conflict. A
// counterexample here would mean the static proof is unsound — the whole thesis. Deterministic seed.
func TestVerifiedCompleteImpliesNoFallthrough(t *testing.T) {
	src := `model "m" {}
input n : number in [0..100]
decision band : string {
  needs: n
  hit: unique
  [0..50)   => "low"
  [50..100] => "high"
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
		if f.Severity == verify.SevError {
			t.Fatalf("model must be provably complete & conflict-free; got blocker %+v", f)
		}
	}
	r := rand.New(rand.NewSource(1))
	check := func(n string) {
		out, err := engine.Eval(cm, "band", map[string]any{"n": json.Number(n)})
		if err != nil {
			t.Fatalf("verified table errored at n=%s: %v", n, err)
		}
		if out == nil {
			t.Fatalf("verified-COMPLETE table fell through to null at n=%s — verification is UNSOUND", n)
		}
	}
	// boundary values + dense random sweep across [0..100]
	for _, b := range []string{"0", "49.999999", "50", "50.000001", "100", "99.999999"} {
		check(b)
	}
	for i := 0; i < 5000; i++ {
		check(strconv.FormatFloat(r.Float64()*100, 'f', 6, 64))
	}
}

// TestPowerMatchesBigIntProperty cross-checks power(x, n) against an independent exact oracle
// (math/big integer exponentiation) over random integer x∈[-10,10], n∈[0,8].
func TestPowerMatchesBigIntProperty(t *testing.T) {
	src := `model "m" {}
input x : number
input n : number
decision p : number = power(x, n)`
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 400; i++ {
		x := int64(r.Intn(21) - 10)
		n := int64(r.Intn(9))
		want := new(big.Int).Exp(big.NewInt(x), big.NewInt(n), nil) // exact x^n
		out, err := engine.Run(src, "p",
			map[string]any{"x": json.Number(strconv.FormatInt(x, 10)), "n": json.Number(strconv.FormatInt(n, 10))})
		if err != nil {
			t.Fatalf("power(%d,%d): %v", x, n, err)
		}
		d, ok := out.(*apd.Decimal)
		if !ok {
			t.Fatalf("power(%d,%d): expected decimal, got %T", x, n, out)
		}
		if got := d.Text('f'); got != want.String() {
			t.Errorf("power(%d,%d) = %s, want %s", x, n, got, want.String())
		}
	}
}
