package engine_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
)

// FuzzEval asserts that evaluating a compiled model against ARBITRARY input JSON never panics — only
// errors or values. The model exercises the arithmetic VM, the new builtins (power), a string predicate,
// and a decision table, so the fuzzer reaches the opcode interpreter and the cell matcher. The model is
// compiled once; only the input is fuzzed.
func FuzzEval(f *testing.F) {
	const src = `model "m" {}
input x : number
input s : string
decision a : number = power(x, 2)
decision b : boolean = starts_with(s, "EU")
decision c : number {
  needs: x
  hit: first
  >= 0 => 1
  default => 0
}`
	m, err := dsl.Parse(src)
	if err != nil {
		f.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(`{"x": 5, "s": "EU-1"}`)
	f.Add(`{"x": -3.5}`)
	f.Add(`{"s": ""}`)
	f.Add(`{}`)
	f.Add(`not json`)
	f.Fuzz(func(_ *testing.T, inputJSON string) {
		var in map[string]any
		d := json.NewDecoder(strings.NewReader(inputJSON))
		d.UseNumber() // decimals stay exact (mirror the real entrypoints)
		if d.Decode(&in) != nil {
			return // invalid JSON: nothing to evaluate
		}
		for _, dec := range []string{"a", "b", "c"} {
			_, _ = engine.Eval(cm, dec, in) // must not panic; an error is fine
		}
	})
}
