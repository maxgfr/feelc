package dsl_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/dsl"
)

// Regression (fork parser): an explicit `?` in an expression made the upstream
// FEEL parser loop forever (unbounded growth → OOM ~100 GB). The fork consumes the token.
// We keep a short timeout so any regression FAILS FAST rather than saturating RAM.
func TestParseDoesNotHangOnExplicitQuestionMark(t *testing.T) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = dsl.Parse("model \"m\" {}\nbkm bad(x:number):number = ? + x\n")
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("dsl.Parse looped on an explicit `?` — regression of the fork parser.go fix")
	}
}

// Col must be set (1-based) for each cell, computed at DSL split time:
// the column points to the first character of the trimmed content in the source line.
func TestCellColumnsFilled(t *testing.T) {
	const ruleLine = "  >= 1 | < 2 => 10"
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"input b : number\n" +
		"decision d : number {\n" +
		"  needs: a, b\n" +
		"  hit: first\n" +
		ruleLine + "\n" +
		"  default => 0\n" +
		"}\n"
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	r := m.Decisions[0].Rules[0]
	if len(r.Conds) != 2 || len(r.Outputs) != 1 {
		t.Fatalf("unexpected structure: %d conds, %d outputs", len(r.Conds), len(r.Outputs))
	}
	if want := strings.Index(ruleLine, ">=") + 1; r.Conds[0].Col != want {
		t.Errorf("Conds[0].Col = %d, expected %d", r.Conds[0].Col, want)
	}
	if want := strings.Index(ruleLine, "<") + 1; r.Conds[1].Col != want {
		t.Errorf("Conds[1].Col = %d, expected %d", r.Conds[1].Col, want)
	}
	if want := strings.Index(ruleLine, "10") + 1; r.Outputs[0].Col != want {
		t.Errorf("Outputs[0].Col = %d, expected %d", r.Outputs[0].Col, want)
	}
}

// ParseFile stamps the file name onto the structured error, with a stable code
// and an actionable suggestion.
func TestParseFileStampsStructuredError(t *testing.T) {
	_, err := dsl.ParseFile("m.rules", "model \"m\" {}\nbogus instruction\n")
	if err == nil {
		t.Fatal("expected error")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("unstructured error: %T %v", err, err)
	}
	if de.File != "m.rules" {
		t.Errorf("File = %q, expected m.rules", de.File)
	}
	if de.Line != 2 {
		t.Errorf("Line = %d, expected 2", de.Line)
	}
	if de.Code != diag.CodeUnknownStmt {
		t.Errorf("Code = %q, expected %q", de.Code, diag.CodeUnknownStmt)
	}
	if de.Suggestion == "" {
		t.Errorf("expected non-empty suggestion")
	}
	// Text compat: without a file, the historical "line N:" prefix.
	_, err2 := dsl.Parse("model \"m\" {}\nbogus instruction\n")
	if !strings.HasPrefix(err2.Error(), "line 2: ") {
		t.Errorf("expected historical text format, got %q", err2.Error())
	}
}

// An invalid FEEL cell produces a diag.Error with code DSL002 wrapping the FEEL cause.
func TestInvalidFeelCellWrapped(t *testing.T) {
	src := "model \"m\" {}\n" +
		"input a : number\n" +
		"decision d : number {\n" +
		"  needs: a\n" +
		"  hit: first\n" +
		"  1 + => 1\n" +
		"}\n"
	_, err := dsl.Parse(src)
	if err == nil {
		t.Fatal("expected error on invalid FEEL cell")
	}
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatalf("unstructured error: %T", err)
	}
	if de.Code != diag.CodeFeelSyntax {
		t.Errorf("Code = %q, expected %q", de.Code, diag.CodeFeelSyntax)
	}
	if de.Cause == nil {
		t.Errorf("the FEEL cause must be wrapped")
	}
}
