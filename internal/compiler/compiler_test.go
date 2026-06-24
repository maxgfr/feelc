package compiler_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dsl"
)

func compileSrc(t *testing.T, src string) error {
	t.Helper()
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	_, err = compiler.Compile(m)
	return err
}

// A mixed-type enum domain (a member whose type differs from the declared input type) must be a
// positioned compile error — otherwise the verifier silently drops the mistyped member (NE-1).
func TestEnumMemberTypeMismatch(t *testing.T) {
	err := compileSrc(t, "model \"m\" {}\n"+
		"input n : number in {1, 2, \"x\"}\n"+ // line 2: "x" is not a number
		"decision d : number = n\n")
	if err == nil {
		t.Fatal("expected a compile error for the mistyped enum member \"x\"")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("unstructured error: %T %v", err, err)
	}
	if de.Code != diag.CodeInputSyntax {
		t.Errorf("code = %s, want %s", de.Code, diag.CodeInputSyntax)
	}
	if !strings.Contains(err.Error(), "does not match the declared input type") {
		t.Errorf("message %q lacks the type-mismatch explanation", err.Error())
	}
	// A correctly-typed numeric enum still compiles.
	if err := compileSrc(t, "model \"m\" {}\ninput n : number in {1, 2, 3}\ndecision d : number = n\n"); err != nil {
		t.Errorf("well-typed numeric enum should compile, got %v", err)
	}
}

// An undeclared `needs` reference must produce a positioned diag.Error (line of the
// decision) with stable code CMP001, while preserving the substring "not declared".
func TestNeedsUndeclaredStructured(t *testing.T) {
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"decision d : string {\n" + // line 3
		"  needs: b\n" +
		"  hit: first\n" +
		"  - => \"x\"\n" +
		"}\n"
	err := compileSrc(t, src)
	if err == nil {
		t.Fatal("expected error")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("unstructured error: %T %v", err, err)
	}
	if de.Code != diag.CodeUndeclared {
		t.Errorf("Code = %q, expected %q", de.Code, diag.CodeUndeclared)
	}
	if de.Line != 3 {
		t.Errorf("Line = %d, expected 3 (line of the decision)", de.Line)
	}
	if !strings.Contains(err.Error(), "not declared") {
		t.Errorf("the historical substring \"not declared\" must be preserved: %q", err.Error())
	}
	if de.Suggestion == "" {
		t.Errorf("a suggestion (valid names) is expected")
	}
}

// An unsupported hit policy must produce CMP002 and preserve "unsupported hit policy".
func TestHitPolicyUnsupportedStructured(t *testing.T) {
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"decision d : string {\n" +
		"  needs: a\n" +
		"  hit: collect avg\n" +
		"  - => \"x\"\n" +
		"}\n"
	err := compileSrc(t, src)
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("unstructured error: %T %v", err, err)
	}
	if de.Code != diag.CodeHitPolicy {
		t.Errorf("Code = %q, expected %q", de.Code, diag.CodeHitPolicy)
	}
	if !strings.Contains(err.Error(), "unsupported hit policy") {
		t.Errorf("historical substring expected: %q", err.Error())
	}
}
