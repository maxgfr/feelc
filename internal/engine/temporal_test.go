package engine_test

import (
	"encoding/json"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

const temporalSrc = `model "leave" {}
input start_date : date
input end_date : date
input notice : duration
decision span : duration = end_date - start_date
decision return_date : date = end_date + notice
decision long_enough : boolean = end_date - start_date >= duration("P14D")
decision after_y2k : boolean = start_date >= date("2000-01-01")`

func temporalModel(t *testing.T) *ir.CompiledModel {
	t.Helper()
	m, err := dsl.Parse(temporalSrc)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	return cm
}

func TestTemporalArithmetic(t *testing.T) {
	cm := temporalModel(t)
	in := map[string]any{"start_date": "2024-01-01", "end_date": "2024-01-31", "notice": "P7D"}
	cases := map[string]string{
		"span":        `"P30D"`,       // 2024-01-31 - 2024-01-01
		"return_date": `"2024-02-07"`, // 2024-01-31 + 7 days
		"long_enough": `true`,         // 30 >= 14
		"after_y2k":   `true`,
	}
	for dec, want := range cases {
		out, err := engine.Eval(cm, dec, in)
		if err != nil {
			t.Fatalf("%s: %v", dec, err)
		}
		b, _ := json.Marshal(canon(out))
		if string(b) != want {
			t.Errorf("%s = %s, want %s", dec, b, want)
		}
	}
}

// Date/duration constants must survive .ir.bin serialization (codec tag-with-Num payload).
func TestTemporalCodecRoundTrip(t *testing.T) {
	cm := temporalModel(t)
	blob, err := ir.Encode(cm)
	if err != nil {
		t.Fatal(err)
	}
	cm2, err := ir.Decode(blob)
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Eval(cm2, "after_y2k", map[string]any{"start_date": "1999-12-31", "end_date": "2024-01-01", "notice": "P0D"})
	if err != nil {
		t.Fatal(err)
	}
	if out != false { // 1999 < 2000 -> the date("2000-01-01") const round-tripped
		t.Errorf("after_y2k(1999) = %v, want false (date const lost in codec?)", out)
	}
}

func TestTemporalBadInput(t *testing.T) {
	cm := temporalModel(t)
	if _, err := engine.Eval(cm, "span", map[string]any{"start_date": "nope", "end_date": "2024-01-31", "notice": "P7D"}); err == nil {
		t.Fatal("invalid date input must error")
	}
	// A date input given as a non-string must error loudly (never silently mis-typed).
	if _, err := engine.Eval(cm, "span", map[string]any{"start_date": 42, "end_date": "2024-01-31", "notice": "P7D"}); err == nil {
		t.Fatal("non-string date input must error")
	}
}

// Unsupported temporal combinations are loud errors, never silent nonsense (ADR 0014).
func TestTemporalUnsupportedCombos(t *testing.T) {
	cases := map[string]string{
		"date_plus_date": `decision d : date = start_date + end_date`,
		"duration_times": `decision d : duration = notice * 2`,
		"date_div":       `decision d : number = start_date / 2`,
		"floor_of_date":  `decision d : number = floor(start_date)`,
	}
	base := `model "m" {}
input start_date : date
input end_date : date
input notice : duration
`
	for name, decl := range cases {
		m, err := dsl.Parse(base + decl)
		if err != nil {
			t.Fatalf("%s parse: %v", name, err)
		}
		cm, err := compiler.Compile(m)
		if err != nil {
			continue // a compile-time rejection is also an acceptable loud failure
		}
		if _, err := engine.Eval(cm, "d", map[string]any{"start_date": "2024-01-01", "end_date": "2024-02-01", "notice": "P7D"}); err == nil {
			t.Errorf("%s: expected a loud error, got none", name)
		}
	}
}

// Day-count overflow is a loud error, not a silently wrapped negative value.
func TestTemporalOverflow(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
input a : duration
input b : duration
decision sum : duration = a + b`)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	big := "P9223372036854775807D" // math.MaxInt64 days
	if _, err := engine.Eval(cm, "sum", map[string]any{"a": big, "b": "P1D"}); err == nil {
		t.Fatal("duration overflow must error loudly, not wrap")
	}
}

// A BKM may not shadow a reserved built-in name (date/duration/floor/…).
func TestReservedBKMName(t *testing.T) {
	m, err := dsl.Parse(`model "m" {}
bkm date(x:number):number = x
decision d : number = date(1)`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := compiler.Compile(m); err == nil {
		t.Fatal("a BKM named `date` must be rejected (reserved built-in)")
	}
}
