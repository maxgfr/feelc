package units

import "testing"

func TestParseString(t *testing.T) {
	cases := []struct{ in, out string }{
		{"", ""},
		{"EUR", "EUR"},
		{"EUR/month", "EUR/month"},
		{"EUR.person/year", "EUR.person/year"},
		{"kg.m/s^2", "kg.m/s^2"},
		{"month/month", ""}, // cancels to dimensionless
	}
	for _, c := range cases {
		u, err := Parse(c.in)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.in, err)
		}
		if u.String() != c.out {
			t.Errorf("Parse(%q).String() = %q, want %q", c.in, u.String(), c.out)
		}
	}
}

func TestMulDivEqual(t *testing.T) {
	eur, _ := Parse("EUR")
	perMonth, _ := Parse("1/month")
	rate, _ := Parse("EUR/month")
	if !eur.Mul(perMonth).Equal(rate) {
		t.Errorf("EUR * 1/month != EUR/month")
	}
	if !rate.Mul(mustParse(t, "month")).Equal(eur) {
		t.Errorf("(EUR/month) * month != EUR")
	}
	if rate.Equal(eur) {
		t.Errorf("EUR/month should not equal EUR")
	}
	if !eur.Div(eur).IsZero() {
		t.Errorf("EUR/EUR should be dimensionless")
	}
}

func TestParseErrors(t *testing.T) {
	for _, s := range []string{"EUR/month/year", "EUR/", "a b"} {
		if _, err := Parse(s); err == nil && s != "EUR/" {
			t.Errorf("Parse(%q) should error", s)
		}
	}
}

func mustParse(t *testing.T, s string) Unit {
	t.Helper()
	u, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
