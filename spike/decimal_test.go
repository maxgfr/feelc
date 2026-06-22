package main

// Spike Slice 0 — confirm that cockroachdb/apd/v3 provides:
//   (1) EXACT DECIMAL arithmetic (no binary surprise like 0.1+0.2 != 0.3);
//   (2) deterministic HALF_EVEN (banker's) rounding, required by feelc.
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
		t.Fatalf("0.1 + 0.2 = %s, expected 0.3 EXACT (failure = binary arithmetic)", got.Text('f'))
	}
	// dti = monthly_debt / (annual_income / 12) with values from the credit model
	tmp := new(apd.Decimal)
	dti := new(apd.Decimal)
	if _, err := ctx.Quo(tmp, mustDec(t, "60000"), mustDec(t, "12")); err != nil { // monthly income
		t.Fatal(err)
	}
	if _, err := ctx.Quo(dti, mustDec(t, "1500"), tmp); err != nil { // 1500 / 5000
		t.Fatal(err)
	}
	if dti.Cmp(mustDec(t, "0.3")) != 0 {
		t.Fatalf("dti = %s, expected 0.3", dti.Text('f'))
	}
}

func TestDecimalHalfEven(t *testing.T) {
	ctx := apd.BaseContext.WithPrecision(34)
	ctx.Rounding = apd.RoundHalfEven
	cases := []struct{ in, want string }{
		{"2.5", "2"},      // .5 -> lower even
		{"3.5", "4"},      // .5 -> upper even
		{"2.125", "2.12"}, // 2 even -> stays (2 decimals)
		{"2.135", "2.14"}, // 3 odd -> rounds up (2 decimals)
	}
	for _, c := range cases {
		got := new(apd.Decimal)
		var exp int32 // 0 decimals by default
		if c.in == "2.125" || c.in == "2.135" {
			exp = -2 // 2 decimals
		}
		if _, err := ctx.Quantize(got, mustDec(t, c.in), exp); err != nil {
			t.Fatalf("Quantize(%s): %v", c.in, err)
		}
		if got.Cmp(mustDec(t, c.want)) != 0 {
			t.Errorf("HALF_EVEN(%s) = %s, expected %s", c.in, got.Text('f'), c.want)
		}
	}
}
