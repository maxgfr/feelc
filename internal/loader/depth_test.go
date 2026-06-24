package loader

import (
	"errors"
	"strings"
	"testing"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/diag"
)

// TestParseDepthLimit asserts that pathologically deep nesting is rejected with a clean, positioned
// compile error (DSL002) instead of overflowing the goroutine stack — a fatal crash recover() cannot
// catch. Each case nests far beyond the parser's maxParseDepth (=400), so the guard must fire. The test
// reaching its assertions at all is itself the proof that no stack overflow occurred.
func TestParseDepthLimit(t *testing.T) {
	const n = 5000
	cases := []struct {
		name string
		src  string
	}{
		{"nested-parens", "decision d : number = " + strings.Repeat("(", n) + "1" + strings.Repeat(")", n) + "\n"},
		{"nested-index", "input a : number\ndecision d : number = " + strings.Repeat("a[", n) + "0" + strings.Repeat("]", n) + "\n"},
		{"nested-array", "decision d : number = " + strings.Repeat("[", n) + "1" + strings.Repeat("]", n) + "\n"},
		{"chained-for", "decision d : number = for " + strings.Repeat("x in [1], ", n) + "x in [1] return 1\n"},
		{"nested-if", "decision d : boolean = " + strings.Repeat("if true then ", n) + "true" + strings.Repeat(" else false", n) + "\n"},
		{"nested-funcall", "decision d : number = " + strings.Repeat("f(", n) + "1" + strings.Repeat(")", n) + "\n"},
		{"nested-some", "decision d : boolean = " + strings.Repeat("some x in [1] satisfies ", n) + "true\n"},
		{"nested-every", "decision d : boolean = " + strings.Repeat("every x in [1] satisfies ", n) + "true\n"},
		{"nested-not", "decision d : boolean = " + strings.Repeat("not(", n) + "true" + strings.Repeat(")", n) + "\n"},
		{"mixed", "decision d : number = " + strings.Repeat("(1 + [", n) + "1" + strings.Repeat("])", n) + "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := Compile([]byte(tc.src))
			if err == nil {
				t.Fatal("expected a depth-limit error, got nil (parser accepted pathological nesting)")
			}
			if !strings.Contains(err.Error(), "nested too deeply") {
				t.Fatalf("expected a 'nested too deeply' error, got: %v", err)
			}
			var de *diag.Error
			if errors.As(err, &de) && de.Code != diag.CodeFeelSyntax {
				t.Errorf("expected diag code %s (DSL002), got %s", diag.CodeFeelSyntax, de.Code)
			}
		})
	}
}

// TestParserDepthLimitDirect exercises the parser's depth guard at its true boundary (feel.ParseString),
// covering FEEL constructs the .rules DSL intercepts before they reach the parser — notably context/map
// literals ({a:{a:…}}), which the DSL rejects as a stray header brace. parseMapNode routes every value
// through expression(), so the guard applies; this asserts it directly. Reaching the assertions proves no
// stack overflow occurred.
func TestParserDepthLimitDirect(t *testing.T) {
	const n = 5000
	cases := []struct {
		name string
		src  string
	}{
		{"nested-context", strings.Repeat("{a:", n) + "1" + strings.Repeat("}", n)},
		{"nested-parens", strings.Repeat("(", n) + "1" + strings.Repeat(")", n)},
		{"nested-list", strings.Repeat("[", n) + "1" + strings.Repeat("]", n)},
		{"chained-for", "for " + strings.Repeat("x in [1], ", n) + "x in [1] return 1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := feel.ParseString(tc.src)
			if err == nil {
				t.Fatal("expected a depth-limit error, got nil (parser accepted pathological nesting)")
			}
			if !strings.Contains(err.Error(), "nested too deeply") {
				t.Fatalf("expected a 'nested too deeply' error, got: %v", err)
			}
		})
	}
}
