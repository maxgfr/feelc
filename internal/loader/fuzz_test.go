package loader

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/ir"
)

// A minimal valid source used to seed the fuzzers (kept here so the targets are self-contained — no
// filesystem reads, no import cycles: loader already depends on dsl/compiler/verify/ir).
const fuzzSeedSrc = "model \"m\" {\n  rounding: half_even\n}\ninput x : number\ndecision d : number = x + 1\n"

// FuzzCompile asserts that the whole front-end (parse → compile → verify → hash, via Compile) NEVER
// panics on arbitrary input — it may only return an error. This covers the parser, the typechecker and
// the lowering on adversarial-but-arbitrary bytes.
func FuzzCompile(f *testing.F) {
	f.Add([]byte(fuzzSeedSrc))
	f.Add([]byte("model \"t\" {}\ndecision d : number {\n  needs: x\n  hit: first\n  >= 1 => 1\n  default => 0\n}\n"))
	f.Add([]byte("input x : number in [0..10]\n"))
	// Deeply nested expressions exercise the parser's recursion-depth guard: each of these crosses
	// maxParseDepth so the front-end must return a clean error, never overflow the stack (a fatal crash).
	f.Add([]byte("decision d : number = " + strings.Repeat("(", 600) + "1" + strings.Repeat(")", 600) + "\n"))
	f.Add([]byte("input a : number\ndecision d : number = " + strings.Repeat("a[", 600) + "0" + strings.Repeat("]", 600) + "\n"))
	f.Add([]byte("decision d : number = for " + strings.Repeat("x in [1], ", 600) + "x in [1] return 1\n"))
	f.Add([]byte(""))
	f.Fuzz(func(_ *testing.T, src []byte) {
		_, _, _, _ = Compile(src) // must not panic; an error result is fine
	})
}

// FuzzExprLower targets the literal-expression lowering path — including the post-ADR-0020..0025
// builtins (round/modulo/power), string predicates and the bounded-quantifier macro: an arbitrary
// expression body must compile to a clean error or a program, never panic.
func FuzzExprLower(f *testing.F) {
	for _, e := range []string{
		"x + 1", "power(x, 3)", "round(x, 2)", "modulo(x, 2)", "abs(x - y)",
		"starts_with(s, \"a\")", "contains(s, \"-\")",
		"every of {x, y} satisfies ? < 10", "some of {x} satisfies ? > 0",
		"if x > 0 then power(x, 2) else 0",
	} {
		f.Add(e)
	}
	const head = "model \"m\" {}\ninput x : number\ninput y : number\ninput s : string\ndecision d : number = "
	f.Fuzz(func(_ *testing.T, expr string) {
		_, _, _, _ = Compile([]byte(head + expr + "\n")) // must not panic; an error is fine
	})
}

// FuzzDecodeIR asserts that decoding an UNTRUSTED .ir.bin blob never panics (the codec-hardening claim of
// ADR 0006). The seed is a real encoding; go-fuzz then mutates the bytes. A blob that decodes cleanly must
// also re-encode without panicking.
func FuzzDecodeIR(f *testing.F) {
	if cm, _, _, err := Compile([]byte(fuzzSeedSrc)); err == nil {
		if blob, err := ir.Encode(cm); err == nil {
			f.Add(blob)
		}
	}
	f.Add([]byte{})
	f.Add([]byte("not an ir blob"))
	f.Fuzz(func(_ *testing.T, blob []byte) {
		cm, err := ir.Decode(blob)
		if err == nil && cm != nil {
			_, _ = ir.Encode(cm) // a successfully-decoded model must round-trip without panicking
		}
	})
}
