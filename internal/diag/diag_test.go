package diag_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/diag"
)

// Error() must stay COMPATIBLE with the historical text format when no file is
// known: "line N: <message>" (the existing tests match these substrings). No
// suggestion must leak into Error().
func TestErrorTextCompatNoFile(t *testing.T) {
	e := diag.New("DSL001", 3, "unrecognized statement").WithSuggestion("try `input`")
	if got, want := e.Error(), "line 3: unrecognized statement"; got != want {
		t.Fatalf("Error() = %q, expected %q", got, want)
	}
	if strings.Contains(e.Error(), "try") {
		t.Fatalf("the suggestion must not appear in Error(): %q", e.Error())
	}
}

// With a file and a column, Error() renders "file:line:col: message".
func TestErrorTextWithFileAndCol(t *testing.T) {
	e := diag.New("DSL002", 3, "invalid FEEL cell").WithFile("m.rules").WithCol(5)
	if got, want := e.Error(), "m.rules:3:5: invalid FEEL cell"; got != want {
		t.Fatalf("Error() = %q, expected %q", got, want)
	}
}

// Col=0 (unknown) -> "file:line: message" (no :0:).
func TestErrorTextWithFileNoCol(t *testing.T) {
	e := diag.New("DSL002", 3, "invalid FEEL cell").WithFile("m.rules")
	if got, want := e.Error(), "m.rules:3: invalid FEEL cell"; got != want {
		t.Fatalf("Error() = %q, expected %q", got, want)
	}
}

// Line=0 (error without position) -> bare message, no prefix.
func TestErrorTextNoLine(t *testing.T) {
	e := diag.New("DSL000", 0, `model without "model" declaration`)
	if got, want := e.Error(), `model without "model" declaration`; got != want {
		t.Fatalf("Error() = %q, expected %q", got, want)
	}
}

// A wrapped cause (historical %w) is rendered after ": " and propagates via Unwrap/errors.As.
func TestWrapAppendsCauseAndUnwraps(t *testing.T) {
	cause := errors.New("unexpected token")
	e := diag.Wrap("DSL003", 5, `invalid FEEL expression "x +"`, cause)
	if got, want := e.Error(), `line 5: invalid FEEL expression "x +": unexpected token`; got != want {
		t.Fatalf("Error() = %q, expected %q", got, want)
	}
	if !errors.Is(e, cause) {
		t.Fatalf("errors.Is does not find the cause")
	}
}

// MarshalJSON produces {file,line,col,code,message,suggestion} with omitempty
// on file/col/suggestion; line and message always present.
func TestMarshalJSONShape(t *testing.T) {
	e := diag.New("DSL001", 3, "unrecognized statement").
		WithFile("m.rules").WithCol(5).WithSuggestion("try `input`")
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"file", "line", "col", "code", "message", "suggestion"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing JSON field: %q (json=%s)", k, b)
		}
	}
	if got["message"] != "unrecognized statement" {
		t.Errorf("message = %v", got["message"])
	}
}

// omitempty: without file/col/suggestion, these fields disappear from the JSON.
func TestMarshalJSONOmitEmpty(t *testing.T) {
	e := diag.New("DSL000", 0, "global error")
	b, _ := json.Marshal(e)
	s := string(b)
	for _, k := range []string{`"file"`, `"col"`, `"suggestion"`} {
		if strings.Contains(s, k) {
			t.Errorf("field %s should not appear (omitempty): %s", k, s)
		}
	}
	// message always present.
	if !strings.Contains(s, `"message"`) {
		t.Errorf("message missing: %s", s)
	}
}

// errors.As lets the CLI recover the structure for the --json output.
func TestErrorsAsStructured(t *testing.T) {
	var err error = diag.New("DSL001", 3, "boom").WithCol(2)
	var de *diag.Error
	if !errors.As(err, &de) {
		t.Fatal("errors.As did not find *diag.Error")
	}
	if de.Line != 3 || de.Col != 2 || de.Code != "DSL001" {
		t.Errorf("incorrect fields: %+v", de)
	}
}
