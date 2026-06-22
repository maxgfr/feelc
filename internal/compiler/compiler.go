// Package compiler transforms a *model.Model (DSL output) into an executable
// *ir.CompiledModel: typecheck (scope gatekeeper) + lowering (normalizing cells
// into CellTest, compiling expressions into bytecode).
//
// Anti-scope-creep discipline: any construct outside the v2 subset fails outright.
// Errors are positioned *diag.Error values (line, and column when they come from a cell),
// with a stable CMPxxx code and a suggestion when useful.
package compiler

import (
	"fmt"
	"sort"
	"strings"

	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/model"
)

// Compile typechecks then lowers the conceptual model into IR.
func Compile(m *model.Model) (*ir.CompiledModel, error) {
	cm := &ir.CompiledModel{Name: m.Name, Inputs: map[string]ir.Type{}, Domains: map[string]ir.Domain{}}
	for _, in := range m.Inputs {
		cm.Inputs[in.Name] = irType(in.Type)
		dom, err := parseDomain(in.Domain)
		if err != nil {
			return nil, diag.Wrap(diag.CodeInputSyntax, in.Line, fmt.Sprintf("input %q", in.Name), err)
		}
		cm.Domains[in.Name] = dom
	}
	// Resolvable names: external inputs + decisions (a cell/expr may reference an upstream decision).
	// BKMs are NOT resolvable names (not in `valid`): they are pure functions,
	// referenceable only via invocation `name(...)`, inlined by the lowerer.
	valid := map[string]bool{}
	for n := range cm.Inputs {
		valid[n] = true
	}
	for _, d := range m.Decisions {
		valid[d.Name] = true
	}
	bkms := map[string]model.BKM{}
	for _, b := range m.BKMs {
		bkms[b.Name] = b
	}
	for _, d := range m.Decisions {
		dec, err := compileDecision(m, valid, bkms, d)
		if err != nil {
			return nil, err
		}
		cm.Decisions = append(cm.Decisions, dec)
	}
	return cm, nil
}

func compileDecision(m *model.Model, valid map[string]bool, bkms map[string]model.BKM, d model.Decision) (ir.Decision, error) {
	if d.Expr != nil {
		prog, err := lowerExpr(d.Expr.Node, bkms)
		if err != nil {
			return ir.Decision{}, diag.Wrap(diag.CodeUnsupported, d.Line, fmt.Sprintf("decision %q", d.Name), err).
				WithCol(d.Expr.Col)
		}
		// `?` (column value) only makes sense inside a table cell: outright refusal at
		// compile time (otherwise it would fail only at runtime — including via a BKM argument).
		if progUsesInput(prog) {
			return ir.Decision{}, diag.Newf(diag.CodeUnsupported, d.Line,
				"decision %q: `?` (column value) forbidden in a literal expression — reserved for table cells", d.Name).
				WithCol(d.Expr.Col)
		}
		if err := checkVars(prog.Vars, valid, d.Name, d.Line); err != nil {
			return ir.Decision{}, err
		}
		return ir.Decision{Name: d.Name, Kind: ir.KindLiteralExpr, Expr: prog, ExprSrc: d.Expr.Src, Deps: prog.Vars, Line: d.Line}, nil
	}

	hp, agg, err := parseHitPolicy(d.HitPolicy, d.Line)
	if err != nil {
		return ir.Decision{}, err
	}
	outNames, err := outputNames(m, d)
	if err != nil {
		return ir.Decision{}, err
	}
	if agg != ir.AggNone && agg != ir.AggCount && len(outNames) != 1 {
		return ir.Decision{}, diag.Newf(diag.CodeCollect, d.Line,
			"decision %q: COLLECT aggregation requires a single scalar output", d.Name)
	}
	for _, n := range d.Needs {
		if !valid[n] {
			return ir.Decision{}, diag.Newf(diag.CodeUndeclared, d.Line,
				"decision %q: `needs` references %q, not declared", d.Name, n).
				WithSuggestion("declared names: " + strings.Join(sortedKeys(valid), ", "))
		}
	}
	table := &ir.DecisionTable{Inputs: d.Needs, Outputs: outNames, HitPolicy: hp, Agg: agg}
	if hp == ir.HitPriority {
		if len(outNames) != 1 {
			return ir.Decision{}, diag.Newf(diag.CodePriority, d.Line,
				"decision %q: PRIORITY requires a single scalar output", d.Name)
		}
		if len(d.Priority) == 0 {
			return ir.Decision{}, diag.Newf(diag.CodePriority, d.Line,
				"decision %q: PRIORITY requires a `priority:` line listing the outputs in decreasing priority order", d.Name)
		}
		for _, c := range d.Priority {
			v, err := literalValue(c.Node)
			if err != nil {
				return ir.Decision{}, diag.Wrap(diag.CodeLiteral, c.Line,
					fmt.Sprintf("decision %q: priority value %q", d.Name, c.Src), err).WithCol(c.Col)
			}
			table.Priority = append(table.Priority, v)
		}
	}
	for _, r := range d.Rules {
		outs, err := literalOutputs(r.Outputs, len(outNames), d.Name, r.Line)
		if err != nil {
			return ir.Decision{}, err
		}
		if r.IsDefault {
			table.Default = outs
			continue
		}
		if len(r.Conds) != len(d.Needs) {
			return ir.Decision{}, diag.Newf(diag.CodeArity, r.Line,
				"decision %q: %d conditions for %d `needs` columns", d.Name, len(r.Conds), len(d.Needs))
		}
		rule := ir.Rule{Outputs: outs, Line: r.Line, OutputSrc: outputSrcs(r.Outputs)}
		for _, c := range r.Conds {
			ct, err := normalizeCell(c, valid, bkms, d.Name)
			if err != nil {
				return ir.Decision{}, err
			}
			rule.Conds = append(rule.Conds, ct)
		}
		table.Rules = append(table.Rules, rule)
	}
	return ir.Decision{Name: d.Name, Kind: ir.KindTable, Table: table, Deps: d.Needs, Line: d.Line}, nil
}

// outputSrcs extracts the source text of the output cells (justification trace).
func outputSrcs(cells []model.Cell) []string {
	srcs := make([]string, len(cells))
	for i, c := range cells {
		srcs[i] = c.Src
	}
	return srcs
}

// outputNames derives the output columns from the decision's type:
// scalar builtin -> 1 output (name = decision); context type -> the fields, in order.
func outputNames(m *model.Model, d model.Decision) ([]string, error) {
	switch model.Type(d.TypeName) {
	case model.TypeNumber, model.TypeString, model.TypeBool:
		return []string{d.Name}, nil
	}
	td, ok := m.Type(d.TypeName)
	if !ok {
		return nil, diag.Newf(diag.CodeUnknownType2, d.Line, "decision %q: unknown type %q", d.Name, d.TypeName).
			WithSuggestion("types: number, string, boolean, or a declared `type ... = context { ... }`")
	}
	names := make([]string, len(td.Fields))
	for i, f := range td.Fields {
		names[i] = f.Name
	}
	return names, nil
}

func literalOutputs(cells []model.Cell, want int, dec string, line int) ([]ir.Value, error) {
	if len(cells) != want {
		return nil, diag.Newf(diag.CodeArity, line, "decision %q: %d outputs, %d expected", dec, len(cells), want)
	}
	out := make([]ir.Value, len(cells))
	for i, c := range cells {
		v, err := literalValue(c.Node)
		if err != nil {
			return nil, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("decision %q: output %q", dec, c.Src), err).WithCol(c.Col)
		}
		out[i] = v
	}
	return out, nil
}

// normalizeCell transforms a condition cell into a geometric CellTest (or Op=Prog).
func normalizeCell(c model.Cell, valid map[string]bool, bkms map[string]model.BKM, dec string) (ir.CellTest, error) {
	if c.Dash {
		return ir.CellTest{Op: ir.OpAny, Src: c.Src, Line: c.Line}, nil
	}
	ct, err := normalizeNode(c.Node, c.Src, valid, bkms, dec, c.Line, c.Col)
	if err != nil {
		return ir.CellTest{}, err
	}
	// Justification trace: Src/Line of the top-level cell (not the sub-tests).
	ct.Src = c.Src
	ct.Line = c.Line
	return ct, nil
}

func normalizeNode(node feel.Node, src string, valid map[string]bool, bkms map[string]model.BKM, dec string, line, col int) (ir.CellTest, error) {
	switch n := node.(type) {
	case *feel.RangeNode:
		lo, err := literalValue(n.Start)
		if err != nil {
			return ir.CellTest{}, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("lower bound of %q", src), err).WithCol(col)
		}
		hi, err := literalValue(n.End)
		if err != nil {
			return ir.CellTest{}, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("upper bound of %q", src), err).WithCol(col)
		}
		return ir.CellTest{Op: ir.OpInRange, A: lo, B: hi, AOpen: n.StartOpen, BOpen: n.EndOpen}, nil
	case *feel.MultiTests:
		ct := ir.CellTest{Op: ir.OpInSet}
		for _, el := range n.Elements {
			sub, err := normalizeNode(el, src, valid, bkms, dec, line, col)
			if err != nil {
				return ir.CellTest{}, err
			}
			ct.Sub = append(ct.Sub, sub)
		}
		return ct, nil
	case *feel.Binop:
		if v, ok := n.Left.(*feel.Var); ok && v.Name == "?" {
			if lit, err := literalValue(n.Right); err == nil {
				op, err := mapOp(n.Op, line, col)
				if err != nil {
					return ir.CellTest{}, err
				}
				return ir.CellTest{Op: op, A: lit}, nil
			}
			// "? op <non-literal expr>" (e.g. "< monthly_debt") -> Op=Prog cell
			return progCell(node, valid, bkms, dec, line, col)
		}
		return progCell(node, valid, bkms, dec, line, col)
	case *feel.NumberNode, *feel.StringNode, *feel.BoolNode:
		lit, err := literalValue(node)
		if err != nil {
			return ir.CellTest{}, diag.Wrap(diag.CodeLiteral, line, fmt.Sprintf("cell %q", src), err).WithCol(col)
		}
		return ir.CellTest{Op: ir.OpEq, A: lit}, nil
	case *feel.FunCall:
		// `not(<test>)` in a cell: GEOMETRIC negation (stays analyzable by the checker).
		// not(x) -> negated test(x); not(a, b, ...) -> outside the set {a, b, ...}.
		if v, ok := n.FunRef.(*feel.Var); ok && v.Name == "not" {
			return negateCell(n, src, valid, bkms, dec, line, col)
		}
		// Other invocation (BKM, floor/round) used as a boolean test -> Op=Prog cell.
		return progCell(node, valid, bkms, dec, line, col)
	default:
		return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cell %q: construct not supported in v2", src).WithCol(col)
	}
}

// negateCell normalizes `not(...)` into a cell: an inverted geometric test (Negate), or an
// OpNot applied to the program for a non-geometric test (Op=Prog). Outright failure on kwargs / 0 args.
func negateCell(n *feel.FunCall, src string, valid map[string]bool, bkms map[string]model.BKM, dec string, line, col int) (ir.CellTest, error) {
	if len(n.Args) == 0 {
		return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cell %q: `not()` expects at least one test", src).WithCol(col)
	}
	for _, a := range n.Args {
		if a.Name != "" {
			return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cell %q: `not(...)` does not accept named arguments", src).WithCol(col)
		}
	}
	if len(n.Args) == 1 {
		inner, err := normalizeNode(n.Args[0].Arg, src, valid, bkms, dec, line, col)
		if err != nil {
			return ir.CellTest{}, err
		}
		if inner.Op == ir.OpProg {
			// non-geometric: negation at runtime (OpNot on the boolean result).
			inner.Prog.Code = append(inner.Prog.Code, ir.Instr{Op: ir.OpNot})
			return inner, nil
		}
		inner.Negate = !inner.Negate // toggle (handles not(not(...)))
		return inner, nil
	}
	// not(a, b, ...): outside the set — OR of the sub-tests, the whole thing negated.
	ct := ir.CellTest{Op: ir.OpInSet, Negate: true}
	for _, a := range n.Args {
		sub, err := normalizeNode(a.Arg, src, valid, bkms, dec, line, col)
		if err != nil {
			return ir.CellTest{}, err
		}
		if sub.Op == ir.OpProg {
			return ir.CellTest{}, diag.Newf(diag.CodeUnsupported, line, "cell %q: multi-test `not(...)` requires geometric tests", src).WithCol(col)
		}
		ct.Sub = append(ct.Sub, sub)
	}
	return ct, nil
}

// progCell compiles a cell into a boolean expression (bytecode layer), `?` = column value.
func progCell(node feel.Node, valid map[string]bool, bkms map[string]model.BKM, dec string, line, col int) (ir.CellTest, error) {
	prog, err := lowerExpr(node, bkms)
	if err != nil {
		return ir.CellTest{}, diag.Wrap(diag.CodeUnsupported, line, "cell", err).WithCol(col)
	}
	if err := checkVars(prog.Vars, valid, dec, line); err != nil {
		return ir.CellTest{}, err
	}
	return ir.CellTest{Op: ir.OpProg, Prog: prog}, nil
}

func checkVars(vars []string, valid map[string]bool, dec string, line int) error {
	for _, v := range vars {
		if !valid[v] {
			return diag.Newf(diag.CodeUndeclared, line,
				"decision %q: references %q, not declared (input or decision)", dec, v).
				WithSuggestion("declared names: " + strings.Join(sortedKeys(valid), ", "))
		}
	}
	return nil
}

// literalValue converts a literal FEEL node into a Value (exact re-parse of numbers via apd).
func literalValue(node feel.Node) (ir.Value, error) {
	switch n := node.(type) {
	case *feel.NumberNode:
		d, err := decimal.Parse(n.Value)
		if err != nil {
			return ir.Value{}, err
		}
		return ir.Num(d), nil
	case *feel.StringNode:
		return ir.Str(n.Content()), nil // Content() strips the quotes + unescapes (Value keeps them)
	case *feel.BoolNode:
		return ir.Bool(n.Value), nil
	default:
		return ir.Value{}, diag.Newf(diag.CodeLiteral, 0, "literal expected, got %T", node)
	}
}

func mapOp(op string, line, col int) (ir.Op, error) {
	switch op {
	case "<":
		return ir.OpLt, nil
	case "<=":
		return ir.OpLe, nil
	case ">":
		return ir.OpGt, nil
	case ">=":
		return ir.OpGe, nil
	case "=":
		return ir.OpEq, nil
	case "!=":
		return ir.OpNe, nil
	default:
		return 0, diag.Newf(diag.CodeUnsupported, line, "cell operator not supported in v2: %q", op).WithCol(col)
	}
}

func parseHitPolicy(s string, no int) (ir.HitPolicy, ir.Aggregation, error) {
	switch s {
	case "":
		return 0, 0, diag.New(diag.CodeHitPolicy, no, "`hit:` missing").
			WithSuggestion("e.g.: hit: first")
	case "first":
		return ir.HitFirst, ir.AggNone, nil
	case "unique":
		return ir.HitUnique, ir.AggNone, nil
	case "any":
		return ir.HitAny, ir.AggNone, nil
	case "priority":
		return ir.HitPriority, ir.AggNone, nil
	case "rule order":
		return ir.HitRuleOrder, ir.AggNone, nil
	case "collect":
		return ir.HitCollect, ir.AggNone, nil
	case "collect sum":
		return ir.HitCollect, ir.AggSum, nil
	case "collect min":
		return ir.HitCollect, ir.AggMin, nil
	case "collect max":
		return ir.HitCollect, ir.AggMax, nil
	case "collect count":
		return ir.HitCollect, ir.AggCount, nil
	default:
		return 0, 0, diag.Newf(diag.CodeHitPolicy, no, "unsupported hit policy: %q", s).
			WithSuggestion("policies: first, unique, any, priority, rule order, collect[ sum|min|max|count]")
	}
}

// parseDomain interprets an input domain constraint (`in [a..b]`, `>= 0`, `in {..}`)
// into an ir.Domain for completeness checking. An unrecognized form -> DomNone (no error).
func parseDomain(s string) (ir.Domain, error) {
	rest := strings.TrimSpace(s)
	if rest == "" {
		return ir.Domain{Kind: ir.DomNone}, nil
	}
	if strings.HasPrefix(rest, "in ") {
		rest = strings.TrimSpace(rest[len("in "):])
	}
	// Enum : { v1, v2, ... }
	if strings.HasPrefix(rest, "{") {
		body := strings.TrimSuffix(strings.TrimPrefix(rest, "{"), "}")
		dom := ir.Domain{Kind: ir.DomEnum}
		for _, part := range strings.Split(body, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			node, err := feel.ParseString(part)
			if err != nil {
				return ir.Domain{}, diag.Wrap(diag.CodeFeelSyntax, 0, fmt.Sprintf("enum domain %q", part), err)
			}
			v, err := literalValue(node)
			if err != nil {
				return ir.Domain{}, diag.Wrap(diag.CodeLiteral, 0, fmt.Sprintf("enum domain %q", part), err)
			}
			dom.Enum = append(dom.Enum, v)
		}
		return dom, nil
	}
	node, err := feel.ParseString(rest)
	if err != nil {
		return ir.Domain{Kind: ir.DomNone}, nil // not interpretable -> no domain (degradation)
	}
	switch n := node.(type) {
	case *feel.RangeNode:
		lo, err := literalValue(n.Start)
		if err != nil {
			return ir.Domain{Kind: ir.DomNone}, nil
		}
		hi, err := literalValue(n.End)
		if err != nil {
			return ir.Domain{Kind: ir.DomNone}, nil
		}
		return ir.Domain{Kind: ir.DomNumeric, Lo: lo, Hi: hi, LoOpen: n.StartOpen, HiOpen: n.EndOpen}, nil
	case *feel.Binop:
		if v, ok := n.Left.(*feel.Var); ok && v.Name == "?" {
			lit, err := literalValue(n.Right)
			if err != nil {
				return ir.Domain{Kind: ir.DomNone}, nil
			}
			switch n.Op {
			case ">=":
				return ir.Domain{Kind: ir.DomNumeric, Lo: lit, HiInf: true}, nil
			case ">":
				return ir.Domain{Kind: ir.DomNumeric, Lo: lit, LoOpen: true, HiInf: true}, nil
			case "<=":
				return ir.Domain{Kind: ir.DomNumeric, Hi: lit, LoInf: true}, nil
			case "<":
				return ir.Domain{Kind: ir.DomNumeric, Hi: lit, HiOpen: true, LoInf: true}, nil
			}
		}
	}
	return ir.Domain{Kind: ir.DomNone}, nil
}

func irType(t model.Type) ir.Type {
	switch t {
	case model.TypeString:
		return ir.TypeString
	case model.TypeBool:
		return ir.TypeBool
	default:
		return ir.TypeNumber
	}
}

// sortedKeys returns the true keys of a set, sorted (for deterministic suggestions).
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k, ok := range set {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
