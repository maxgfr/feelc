// Package ir defines the intermediate representation of feelc: a compiled model,
// immutable after compilation, which the VM executes and the verification analyzes.
//
// Target design (cf. plan): 3 stacked layers —
//
//	(1) topologically-sorted decision graph;
//	(2) declarative tables normalized into CellTest (geometric form = analyzable);
//	(3) flat FEEL bytecode per cell/expression (Slice 2+).
//
// Slice 1 only materializes (1) + (2) with literal outputs.
package ir

// Op: normalized operator of a table cell (unary test).
type Op uint8

const (
	OpAny     Op = iota // "-": always true (don't care)
	OpEq                // bare literal: equality
	OpNe                // "!= x"
	OpLt                // "< x"
	OpLe                // "<= x"
	OpGt                // "> x"
	OpGe                // ">= x"
	OpInRange           // "[a..b)" etc. (Slice 2)
	OpInSet             // "a","b"   (Slice 3)
	OpProg              // free expression -> bytecode (Slice 2)
)

// CellTest: normalized form of an input cell. This is exactly the geometry
// (comparison/range/set) that the verifier knows how to decompose.
type CellTest struct {
	Op     Op
	A      Value        // comparand (Eq/Ne/Lt..) or low bound (InRange)
	B      Value        // high bound (InRange)
	AOpen  bool         // low bound excluded?
	BOpen  bool         // high bound excluded?
	Negate bool         // `not(<test>)`: inverts the geometric result (stays analyzable)
	Sub    []CellTest   // OpInSet: OR of sub-tests (comma semantics of DMN unary tests)
	Prog   *ExprProgram // OpProg: cell = free expression (references another column, arithmetic)
	Src    string       // cell source text (justification trace, `explain`)
	Line   int          // 1-based source line (0 if unknown, e.g. loaded from a .ir.bin)
}

// HitPolicy: resolution policy of a decision table (DMN semantics).
type HitPolicy uint8

const (
	HitUnique HitPolicy = iota
	HitAny
	HitFirst
	HitPriority
	HitCollect
	HitRuleOrder
)

// Rule: a table row. Conds aligned on the input columns, Outputs on the outputs.
type Rule struct {
	Conds     []CellTest
	Outputs   []Value  // literals in Slice 1 (expressions in Slice 2)
	Line      int      // 1-based source line of the rule (justification trace)
	OutputSrc []string // source text of the outputs (aligned on Outputs)
}

// Aggregation: aggregation function of a COLLECT hit policy.
type Aggregation uint8

const (
	AggNone  Aggregation = iota // raw COLLECT -> list of outputs
	AggSum                      // C+
	AggMin                      // C<
	AggMax                      // C>
	AggCount                    // C#
)

// DecisionTable: the logic of a decision in table form.
type DecisionTable struct {
	Inputs    []string // names of the input variables, in column order
	Outputs   []string // names of the outputs (1 = scalar output; >1 = context)
	Rules     []Rule
	HitPolicy HitPolicy
	Agg       Aggregation // if HitCollect
	Priority  []Value     // if HitPriority: output values, descending priority order
	Default   []Value     // output of the `default` row (nil if absent)
}

// DecisionKind distinguishes table and literal expression.
type DecisionKind uint8

const (
	KindTable       DecisionKind = iota
	KindLiteralExpr              // Slice 2
)

// Meta holds optional documentation/traceability annotations (@title/@doc/@question/@source).
// It is descriptive only: NOT part of the canonical encoding or hash (two models that differ only
// by metadata are the same computational model), so it is dropped on .ir.bin serialization.
type Meta struct {
	Title    string `json:"title,omitempty"`
	Doc      string `json:"doc,omitempty"`
	Question string `json:"question,omitempty"`
	Source   string `json:"source,omitempty"`
}

func (m Meta) Empty() bool { return m == Meta{} }

// Decision: a node of the decision graph (DRG).
type Decision struct {
	Name    string
	Kind    DecisionKind
	Table   *DecisionTable // if KindTable
	Expr    *ExprProgram   // if KindLiteralExpr
	ExprSrc string         // if KindLiteralExpr: source text of the expression (justification)
	Deps    []string       // dependencies (information requirements)
	Meta    Meta           // documentation/traceability (in-memory only)
	Line    int            // 1-based source line of the decision
}

// Type: static type of a variable.
type Type uint8

const (
	TypeNumber Type = iota
	TypeString
	TypeBool
	TypeContext  // Slice 2
	TypeDate     // calendar date (ADR 0014)
	TypeDuration // whole-day duration (ADR 0014)
)

// DomainKind: nature of an input's domain (for completeness verification).
type DomainKind uint8

const (
	DomNone    DomainKind = iota // no declared domain
	DomNumeric                   // range [Lo, Hi] (bounds possibly infinite)
	DomEnum                      // finite set of values
)

// Domain: domain constraint of an input (from `input x : T in [..]` / `>= 0` / `in {..}`).
type Domain struct {
	Kind           DomainKind
	Lo, Hi         Value // numeric bounds (if DomNumeric)
	LoInf, HiInf   bool  // infinite bound?
	LoOpen, HiOpen bool  // excluded bound?
	Enum           []Value
}

// CompiledModel: the model ready to execute (compiler output).
type CompiledModel struct {
	Name      string
	Inputs    map[string]Type
	Domains   map[string]Domain // domain declared per input (empty = DomNone)
	InputMeta map[string]Meta   // documentation per input (in-memory only; never serialized)
	Units     map[string]string // canonical physical unit per input/decision (in-memory; "" = dimensionless)
	Decisions []Decision

	byName map[string]int
}

// Decision finds a decision by name (lazy index).
func (cm *CompiledModel) Decision(name string) (*Decision, bool) {
	if cm.byName == nil {
		cm.byName = make(map[string]int, len(cm.Decisions))
		for i := range cm.Decisions {
			cm.byName[cm.Decisions[i].Name] = i
		}
	}
	if i, ok := cm.byName[name]; ok {
		return &cm.Decisions[i], true
	}
	return nil, false
}
