package main

// Spike Tranche 0 — confirmer que cockroachdb/apd/v3 fournit :
//   (1) une arithmétique DÉCIMALE EXACTE (pas de surprise binaire type 0.1+0.2 != 0.3) ;
//   (2) l'arrondi HALF_EVEN (banker's rounding) déterministe, exigé par feelc.
// `go test ./spike -run TestDecimal -v`

import (
	"testing"

	apd "github.com/cockroachdb/apd/v3"
)

func mustDec(t *testing.T, s string) *apd.Decimal {
	t.Helper()
	d, _, err := apd.NewFromString(s)
	if err != nil {
		t.Fatalf("NewFromString(%q): %v", s, err)
	}
	return d
}

func TestDecimalExactness(t *testing.T) {
	ctx := apd.BaseContext.WithPrecision(34)
	got := new(apd.Decimal)
	if _, err := ctx.Add(got, mustDec(t, "0.1"), mustDec(t, "0.2")); err != nil {
		t.Fatal(err)
	}
	if got.Cmp(mustDec(t, "0.3")) != 0 {
		t.Fatalf("0.1 + 0.2 = %s, attendu 0.3 EXACT (échec = arithmétique binaire)", got.Text('f'))
	}
	// dti = monthly_debt / (annual_income / 12) avec des valeurs du modèle crédit
	tmp := new(apd.Decimal)
	dti := new(apd.Decimal)
	if _, err := ctx.Quo(tmp, mustDec(t, "60000"), mustDec(t, "12")); err != nil { // revenu mensuel
		t.Fatal(err)
	}
	if _, err := ctx.Quo(dti, mustDec(t, "1500"), tmp); err != nil { // 1500 / 5000
		t.Fatal(err)
	}
	if dti.Cmp(mustDec(t, "0.3")) != 0 {
		t.Fatalf("dti = %s, attendu 0.3", dti.Text('f'))
	}
}

func TestDecimalHalfEven(t *testing.T) {
	ctx := apd.BaseContext.WithPrecision(34)
	ctx.Rounding = apd.RoundHalfEven
	cases := []struct{ in, want string }{
		{"2.5", "2"},   // .5 -> pair inférieur
		{"3.5", "4"},   // .5 -> pair supérieur
		{"2.125", "2.12"}, // 2 pair -> reste (à 2 décimales)
		{"2.135", "2.14"}, // 3 impair -> arrondi sup (à 2 décimales)
	}
	for _, c := range cases {
		got := new(apd.Decimal)
		var exp int32 // 0 décimale par défaut
		if c.in == "2.125" || c.in == "2.135" {
			exp = -2 // 2 décimales
		}
		if _, err := ctx.Quantize(got, mustDec(t, c.in), exp); err != nil {
			t.Fatalf("Quantize(%s): %v", c.in, err)
		}
		if got.Cmp(mustDec(t, c.want)) != 0 {
			t.Errorf("HALF_EVEN(%s) = %s, attendu %s", c.in, got.Text('f'), c.want)
		}
	}
}
