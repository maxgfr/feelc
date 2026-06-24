package engine_test

import (
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
)

// mustCompile fails if a source that documents a supported feature does not parse+compile — the
// "documented ⇒ implemented" guard (doc↔impl parity).
func mustCompile(t *testing.T, label, src string) {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatalf("documented feature %q failed to PARSE:\n%s\nerr: %v", label, src, err)
	}
	if _, err := compiler.Compile(m); err != nil {
		t.Fatalf("documented feature %q failed to COMPILE:\n%s\nerr: %v", label, src, err)
	}
}

// TestDocumentedHitPoliciesImplemented: every hit policy listed in docs/dsl-grammar.md must compile.
func TestDocumentedHitPoliciesImplemented(t *testing.T) {
	singleHit := func(hp string) string {
		return "model \"m\" {}\ninput n : number\ndecision d : number {\n  needs: n\n  hit: " + hp + "\n  >= 0 => 1\n  default => 0\n}"
	}
	listHit := func(hp string) string {
		return "model \"m\" {}\ninput n : number\ndecision d : number {\n  needs: n\n  hit: " + hp + "\n  >= 0 => 1\n  >= 5 => 2\n}"
	}
	ranked := func(hp string) string {
		return "model \"m\" {}\ninput n : number\ndecision d : string {\n  needs: n\n  hit: " + hp + "\n  priority: \"a\", \"b\"\n  >= 0 => \"a\"\n  >= 5 => \"b\"\n}"
	}
	for _, hp := range []string{"first", "unique", "any"} {
		mustCompile(t, "hit:"+hp, singleHit(hp))
	}
	for _, hp := range []string{"rule order", "collect", "collect sum", "collect min", "collect max", "collect count"} {
		mustCompile(t, "hit:"+hp, listHit(hp))
	}
	for _, hp := range []string{"priority", "output order"} {
		mustCompile(t, "hit:"+hp, ranked(hp))
	}
}

// TestDocumentedBuiltinsImplemented: every builtin / predicate / quantifier / temporal form listed in
// docs/feel-subset.md must compile AND run (documented ⇒ implemented ⇒ executable).
func TestDocumentedBuiltinsImplemented(t *testing.T) {
	cases := []struct{ expr, typ string }{
		{"floor(x)", "number"}, {"ceiling(x)", "number"}, {"round(x)", "number"},
		{"round(x, 2)", "number"}, {"abs(x)", "number"}, {"trunc(x)", "number"},
		{"modulo(x, 2)", "number"}, {"power(x, 2)", "number"},
		{"not(x > 0)", "boolean"},
		{"starts_with(s, \"a\")", "boolean"}, {"ends_with(s, \"a\")", "boolean"}, {"contains(s, \"a\")", "boolean"},
		{"every of {x} satisfies ? > 0", "boolean"}, {"some of {x} satisfies ? < 0", "boolean"},
		{"if x > 0 then 1 else 0", "number"},
		{"date(\"2024-01-01\")", "date"}, {"duration(\"P1D\")", "duration"},
	}
	for _, c := range cases {
		src := "model \"m\" {}\ninput x : number\ninput s : string\ndecision d : " + c.typ + " = " + c.expr
		mustCompile(t, c.expr, src)
		if _, err := engine.Run(src, "d", map[string]any{"x": jn("1"), "s": "ab"}); err != nil {
			t.Errorf("documented builtin %q failed to RUN: %v", c.expr, err)
		}
	}
}
