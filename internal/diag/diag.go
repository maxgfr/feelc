// Package diag provides a STRUCTURED, serializable compilation error type
// ({file,line,col,code,message,suggestion}), while remaining backward-compatible with
// the historical text format ("line N: <message>") expected by existing tests.
//
// Project discipline (never conform silently): every error site in the
// parse->compile pipeline produces an explicit diag.Error, with a stable Code (catalogue below)
// and, when useful, a Suggestion usable by the skill's red->green loop.
// The Suggestion is NOT rendered by Error() (so as not to break substring assertions) —
// only in the JSON and a dedicated human-readable rendering.
package diag

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Catalogue of STABLE codes (consumed by the skill and docs/error-schema.md).
// Prefix DSL = parser; CMP = compiler. Do not renumber existing codes.
const (
	// DSL — .rules parser
	CodeUnknownStmt   = "DSL001" // unrecognized statement
	CodeFeelSyntax    = "DSL002" // invalid FEEL cell/expression
	CodeNoModel       = "DSL003" // model without a `model "..."` declaration
	CodeModelHeader   = "DSL004" // malformed `model` header
	CodeInputSyntax   = "DSL005" // malformed `input`
	CodeDecisionHead  = "DSL006" // malformed decision header
	CodeDecisionBody  = "DSL007" // unrecognized decision body line
	CodeRuleSyntax    = "DSL008" // malformed rule
	CodeEmptyCell     = "DSL009" // empty cell
	CodeTypeDecl      = "DSL010" // malformed `type` declaration
	CodeUnknownType   = "DSL011" // unsupported type
	CodeBraceTrailing = "DSL012" // content after `{` on the header line
	CodeBKM           = "DSL013" // malformed `bkm` declaration

	// CMP — compiler / typecheck
	CodeUndeclared   = "CMP001" // reference to a name not declared
	CodeHitPolicy    = "CMP002" // unsupported hit policy
	CodeUnknownType2 = "CMP003" // unknown decision type
	CodeArity        = "CMP004" // wrong number of conditions/outputs
	CodePriority     = "CMP005" // PRIORITY constraint not satisfied
	CodeCollect      = "CMP006" // COLLECT constraint not satisfied
	CodeUnsupported  = "CMP007" // construct outside the v2 subset
	CodeLiteral      = "CMP008" // literal expected
)

// Error is a structured compilation diagnostic, positioned in the source.
type Error struct {
	File       string // source path ("" -> historical text prefix "line N:")
	Line       int    // 1-based line (0 = unknown -> no position prefix)
	Col        int    // 1-based column in the source line (0 = unknown)
	Code       string // stable catalogue code (e.g. "DSL001")
	Message    string // human-readable message (EN); identical to the historical text
	Suggestion string // fix hint (never in Error(), only JSON/human rendering)
	Cause      error  // wrapped underlying error (equivalent of the historical %w)
}

// New creates a positioned diagnostic. line=0 if the position is unknown.
func New(code string, line int, message string) *Error {
	return &Error{Code: code, Line: line, Message: message}
}

// Newf is the format variant of New.
func Newf(code string, line int, format string, args ...any) *Error {
	return &Error{Code: code, Line: line, Message: fmt.Sprintf(format, args...)}
}

// Wrap wraps a cause (equivalent of fmt.Errorf("...: %w")).
func Wrap(code string, line int, message string, cause error) *Error {
	return &Error{Code: code, Line: line, Message: message, Cause: cause}
}

// WithFile sets the source file (chainable).
func (e *Error) WithFile(file string) *Error { e.File = file; return e }

// WithCol sets the 1-based column (chainable).
func (e *Error) WithCol(col int) *Error { e.Col = col; return e }

// WithSuggestion attaches a fix hint (chainable).
func (e *Error) WithSuggestion(s string) *Error { e.Suggestion = s; return e }

// WithCause attaches a wrapped cause (chainable).
func (e *Error) WithCause(c error) *Error { e.Cause = c; return e }

// Error renders the diagnostic in text format. Backward-compatible:
//   - File=="" and Line>0  -> "line N: <message>"
//   - File!=""             -> "file:line[:col]: <message>"
//   - Line==0 and File=="" -> "<message>" (global error without position)
//
// The eventual cause is rendered after ": " (like the historical %w). The suggestion
// NEVER appears here.
func (e *Error) Error() string {
	prefix := ""
	switch {
	case e.File != "" && e.Line > 0:
		if e.Col > 0 {
			prefix = fmt.Sprintf("%s:%d:%d: ", e.File, e.Line, e.Col)
		} else {
			prefix = fmt.Sprintf("%s:%d: ", e.File, e.Line)
		}
	case e.Line > 0:
		prefix = fmt.Sprintf("line %d: ", e.Line)
	}
	msg := e.Message
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return prefix + msg
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *Error) Unwrap() error { return e.Cause }

// MarshalJSON serializes {file,line,col,code,message,suggestion} (omitempty on
// file/col/code/suggestion; line and message always present).
func (e *Error) MarshalJSON() ([]byte, error) {
	type jsonErr struct {
		File       string `json:"file,omitempty"`
		Line       int    `json:"line"`
		Col        int    `json:"col,omitempty"`
		Code       string `json:"code,omitempty"`
		Message    string `json:"message"`
		Suggestion string `json:"suggestion,omitempty"`
	}
	return json.Marshal(jsonErr{
		File: e.File, Line: e.Line, Col: e.Col,
		Code: e.Code, Message: e.Message, Suggestion: e.Suggestion,
	})
}

// WithFileIfDiag stamps the file on the eventual *diag.Error of an error
// chain (no-op if err does not wrap a *diag.Error or if File is already set).
// Single point of file-name propagation as the pipeline unwinds.
func WithFileIfDiag(err error, file string) error {
	if err == nil || file == "" {
		return err
	}
	var de *Error
	if errors.As(err, &de) && de.File == "" && de.Line > 0 {
		de.File = file
	}
	return err
}
