// Package model is the conceptual representation of a rule model, as
// produced by the DSL parser and consumed by the compiler. It retains the FEEL AST
// of cells and their source positions (NL<->rule bridge + explanation).
package model

import feel "github.com/pbinitiative/feel"

// Type: declared scalar type of a variable (source form).
type Type string

const (
	TypeNumber Type = "number"
	TypeString Type = "string"
	TypeBool   Type = "boolean"
)

// Input: a declared input data (DMN Input Data).
type Input struct {
	Name   string
	Type   Type
	Domain string // raw domain constraint (e.g. "in [300..850]", ">= 0"); used by the check (T4)
	Line   int
}

// Field: a field of a context type (multi-column output).
type Field struct {
	Name string
	Type Type
}

// TypeDecl: declaration `type Name = context { f1: t1, f2: t2 }`.
type TypeDecl struct {
	Name   string
	Fields []Field
	Line   int
}

// Cell: a table cell (input condition or output), with its source trace.
type Cell struct {
	Src  string    // raw source text (for the explanation)
	Dash bool      // "-": any/don't-care (never passed to the FEEL parser)
	Node feel.Node // FEEL AST (nil if Dash)
	Line int
	Col  int
}

// Rule: a table row (conditions => outputs), or the `default` row.
type Rule struct {
	Conds     []Cell
	Outputs   []Cell
	IsDefault bool // `default` row: applies when no rule matches
	Line      int
}

// Decision: a DRG decision. Either a table (Rules), or an expression (Expr).
type Decision struct {
	Name      string
	TypeName  string // "number"/"string"/"boolean" or a declared context type name
	Needs     []string
	HitPolicy string
	Priority  []Cell // ordered output values (PRIORITY), from highest priority to lowest
	Rules     []Rule
	Expr      *Cell // if != nil: literal-expression decision (scalar TypeName), no table
	Line      int
}

// BKM: Business Knowledge Model — a named parameterized, reusable expression, inlined
// at compile time by AST substitution of the parameters (zero call frame at runtime).
// Source form: `bkm name(p1:t1, p2:t2):ret = expr`. Referenceable by invocation `name(a, b)`
// in any expression (literal-expr decision or Op=Prog cell).
type BKM struct {
	Name   string
	Params []Field // positional parameters (name + type)
	Ret    Type    // declared return type
	Body   *Cell   // body (Src + FEEL AST)
	Line   int
}

// Model: a complete model.
type Model struct {
	Name      string
	Inputs    []Input
	Types     []TypeDecl
	BKMs      []BKM
	Decisions []Decision
}

// BKM looks up a BKM by name.
func (m *Model) BKM(name string) (BKM, bool) {
	for _, b := range m.BKMs {
		if b.Name == name {
			return b, true
		}
	}
	return BKM{}, false
}

// Type looks up a context type declaration by name.
func (m *Model) Type(name string) (TypeDecl, bool) {
	for _, t := range m.Types {
		if t.Name == name {
			return t, true
		}
	}
	return TypeDecl{}, false
}
