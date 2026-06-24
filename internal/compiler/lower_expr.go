package compiler

import (
	feel "github.com/pbinitiative/feel"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/model"
)

// lowerExpr compiles a FEEL node into an ExprProgram (flat bytecode).
// v2 subset: literals, variables (including `?`), arithmetic +-*/, comparisons, and/or,
// and BKM INVOCATION `name(a, b)` (inlined by AST substitution of parameters — zero
// new opcode, the VM does not know a BKM ever existed). Other constructs (if/then/else,
// for/some/every, **) fail outright.
// Inlining guards (bound compilation RAM against a pathological source: mutual
// recursion, or exponential expansion of acyclic nested BKMs). Symmetric to the
// gridBudget of verification — never conform silently: we fail outright.
const (
	maxInlineDepth = 256     // max depth of the inlining chain
	maxInstrBudget = 200_000 // max number of bytecode instructions emitted for an expression
)

// naSentinel is an internal FEEL var name (unreachable from source — it contains a NUL) that the
// lowerer turns into a non-applicable constant. Applicability lowering injects it as the else/then
// branch of an `if` (see compiler.go).
const naSentinel = "\x00__na__"

func lowerExpr(node feel.Node, bkms map[string]model.BKM) (*ir.ExprProgram, error) {
	l := &lowerer{prog: &ir.ExprProgram{}, varIdx: map[string]int{}, bkms: bkms, recursive: recursiveBKMs(bkms)}
	if err := l.emit(node); err != nil {
		return nil, err
	}
	l.prog.MaxStack = maxStack(l.prog.Code)
	return l.prog, nil
}

type lowerer struct {
	prog        *ir.ExprProgram
	varIdx      map[string]int
	bkms        map[string]model.BKM // known BKMs (inlinable invocations)
	recursive   map[string]bool      // BKMs on a cycle (self/mutual) — invocation forbidden
	inlineDepth int                  // current inlining depth (anti-explosion guard)
}

// recursiveBKMs statically computes the set of BKMs that invoke themselves,
// directly or via a cycle (mutual) — detected by transitive closure of the call graph.
// Distinguishing this STATIC cycle from `f(f(x))` nesting (legitimate, acyclic) is crucial:
// an inlining-stack guard would conflate the two (a call in argument position is NOT
// a cycle). Recursive BKMs are rejected at invocation; the residual graph is acyclic
// so inlining terminates.
func recursiveBKMs(bkms map[string]model.BKM) map[string]bool {
	calls := make(map[string]map[string]bool, len(bkms))
	for name, b := range bkms {
		set := map[string]bool{}
		collectBKMCalls(b.Body.Node, bkms, set)
		calls[name] = set
	}
	rec := map[string]bool{}
	for name := range bkms {
		// name is recursive if it reaches itself in ≥1 edge.
		seen := map[string]bool{}
		var reaches func(cur string) bool
		reaches = func(cur string) bool {
			for callee := range calls[cur] {
				if callee == name {
					return true
				}
				if !seen[callee] {
					seen[callee] = true
					if reaches(callee) {
						return true
					}
				}
			}
			return false
		}
		if reaches(name) {
			rec[name] = true
		}
	}
	return rec
}

// collectBKMCalls collects the names of BKMs invoked in an expression (lowerable subset).
func collectBKMCalls(node feel.Node, bkms map[string]model.BKM, out map[string]bool) {
	switch n := node.(type) {
	case *feel.FunCall:
		if v, ok := n.FunRef.(*feel.Var); ok {
			if _, isBKM := bkms[v.Name]; isBKM {
				out[v.Name] = true
			}
		}
		for _, a := range n.Args {
			collectBKMCalls(a.Arg, bkms, out)
		}
	case *feel.Binop:
		collectBKMCalls(n.Left, bkms, out)
		collectBKMCalls(n.Right, bkms, out)
	case *feel.IfExpr:
		collectBKMCalls(n.Cond, bkms, out)
		collectBKMCalls(n.ThenBranch, bkms, out)
		collectBKMCalls(n.ElseBranch, bkms, out)
	}
}

func (l *lowerer) constIdx(v ir.Value) uint32 {
	idx := uint32(len(l.prog.Consts))
	l.prog.Consts = append(l.prog.Consts, v)
	return idx
}

func (l *lowerer) varIndex(name string) uint32 {
	if i, ok := l.varIdx[name]; ok {
		return uint32(i)
	}
	i := len(l.prog.Vars)
	l.prog.Vars = append(l.prog.Vars, name)
	l.varIdx[name] = i
	return uint32(i)
}

func (l *lowerer) push(op ir.Opcode, arg uint32) {
	l.prog.Code = append(l.prog.Code, ir.Instr{Op: op, Arg: arg})
}

func (l *lowerer) emit(node feel.Node) error {
	if len(l.prog.Code) > maxInstrBudget {
		return diag.Newf(diag.CodeUnsupported, 0,
			"expression too large to compile (> %d instructions) — excessive BKM inlining", maxInstrBudget)
	}
	switch n := node.(type) {
	case *feel.NumberNode:
		d, err := decimal.Parse(n.Value)
		if err != nil {
			return err
		}
		l.push(ir.OpPushConst, l.constIdx(ir.Num(d)))
	case *feel.StringNode:
		l.push(ir.OpPushConst, l.constIdx(ir.Str(n.Content())))
	case *feel.BoolNode:
		l.push(ir.OpPushConst, l.constIdx(ir.Bool(n.Value)))
	case *feel.Var:
		switch n.Name {
		case "?":
			l.push(ir.OpLoadInput, 0)
		case naSentinel:
			// internal marker injected by applicability lowering -> push a non-applicable constant
			l.push(ir.OpPushConst, l.constIdx(ir.NA()))
		default:
			l.push(ir.OpLoadVar, l.varIndex(n.Name))
		}
	case *feel.Binop:
		if err := l.emit(n.Left); err != nil {
			return err
		}
		if err := l.emit(n.Right); err != nil {
			return err
		}
		op, err := binopcode(n.Op)
		if err != nil {
			return err
		}
		l.push(op, 0)
	case *feel.IfExpr:
		return l.emitIf(n)
	case *feel.FunCall:
		return l.emitCall(n)
	default:
		return diag.Newf(diag.CodeUnsupported, 0, "expression not supported in v2: %T", node)
	}
	return nil
}

// monoArgBuiltins: pure mono-arg built-ins supported in expressions. floor/ceiling/abs/trunc map
// onto the frozen decimal context (determinism); `not` is boolean negation. `round` is handled
// separately (it accepts 1 OR 2 args); `modulo` is two-arg (see emitCall).
var monoArgBuiltins = map[string]ir.Opcode{
	"floor":   ir.OpFloor,
	"ceiling": ir.OpCeil,
	"abs":     ir.OpAbs,
	"trunc":   ir.OpTrunc,
	"not":     ir.OpNot,
}

// emitIf compiles `if c then a else b` via backpatch (OpJmpFalse to else, OpJmp to end).
func (l *lowerer) emitIf(n *feel.IfExpr) error {
	if err := l.emit(n.Cond); err != nil {
		return err
	}
	jmpFalse := len(l.prog.Code)
	l.push(ir.OpJmpFalse, 0) // -> else (backpatched)
	if err := l.emit(n.ThenBranch); err != nil {
		return err
	}
	jmpEnd := len(l.prog.Code)
	l.push(ir.OpJmp, 0) // -> end (backpatched)
	elseStart := len(l.prog.Code)
	if err := l.emit(n.ElseBranch); err != nil {
		return err
	}
	end := len(l.prog.Code)
	l.prog.Code[jmpFalse].Arg = uint32(elseStart)
	l.prog.Code[jmpEnd].Arg = uint32(end)
	return nil
}

// emitCall routes an invocation: deterministic built-in (mono- or two-arg), otherwise BKM inlining.
func (l *lowerer) emitCall(fc *feel.FunCall) error {
	ref, ok := fc.FunRef.(*feel.Var)
	if !ok {
		return diag.Newf(diag.CodeUnsupported, 0, "invocation: only `name(...)` is supported, got %s", fc.FunRef.Repr())
	}
	// Deterministic multi-arg built-ins (whitelist carve-out of ADR 0004, see ADR 0020). Genuinely
	// problematic multi-arg builtins (substring, …) still fail outright (they are not whitelisted).
	switch ref.Name {
	case "round":
		// round(x) -> nearest integer; round(x, n) -> n decimal places. Both HALF_EVEN.
		switch {
		case len(fc.Args) == 1 && positionalArgs(fc.Args):
			if err := l.emit(fc.Args[0].Arg); err != nil {
				return err
			}
			l.push(ir.OpRound, 0)
		case len(fc.Args) == 2 && positionalArgs(fc.Args):
			if err := l.emit(fc.Args[0].Arg); err != nil {
				return err
			}
			if err := l.emit(fc.Args[1].Arg); err != nil {
				return err
			}
			l.push(ir.OpRoundN, 0)
		default:
			return diag.Newf(diag.CodeUnsupported, 0, "round expects round(x) or round(x, n) with positional arguments")
		}
		return nil
	case "modulo":
		if len(fc.Args) != 2 || !positionalArgs(fc.Args) {
			return diag.Newf(diag.CodeUnsupported, 0, "modulo expects modulo(x, y) with two positional arguments")
		}
		if err := l.emit(fc.Args[0].Arg); err != nil {
			return err
		}
		if err := l.emit(fc.Args[1].Arg); err != nil {
			return err
		}
		l.push(ir.OpMod, 0)
		return nil
	case "power":
		// power(x, n): integer-exponent exponentiation (exact). The `**`/`^` operators are NOT lexed
		// by the vendored FEEL parser, so this is the FEEL-standard function form (cf. ADR 0020/modulo).
		if len(fc.Args) != 2 || !positionalArgs(fc.Args) {
			return diag.Newf(diag.CodeUnsupported, 0, "power expects power(x, n) with two positional arguments")
		}
		if err := l.emit(fc.Args[0].Arg); err != nil {
			return err
		}
		if err := l.emit(fc.Args[1].Arg); err != nil {
			return err
		}
		l.push(ir.OpPow, 0)
		return nil
	case "starts_with", "ends_with", "contains":
		// Pure, total (string, string) -> boolean predicates for code/policy routing (NOT a
		// string-manipulation library: no substring/upper/replace). Verification-safe: a cell that
		// uses one becomes Op=Prog (non-geometric) and degrades to not-verifiable, never falsely complete.
		if len(fc.Args) != 2 || !positionalArgs(fc.Args) {
			return diag.Newf(diag.CodeUnsupported, 0, "%s expects %s(s, t) with two positional arguments", ref.Name, ref.Name)
		}
		if err := l.emit(fc.Args[0].Arg); err != nil {
			return err
		}
		if err := l.emit(fc.Args[1].Arg); err != nil {
			return err
		}
		l.push(stringPredOpcode(ref.Name), 0)
		return nil
	}
	if op, isBuiltin := monoArgBuiltins[ref.Name]; isBuiltin {
		if len(fc.Args) != 1 || fc.Args[0].Name != "" {
			return diag.Newf(diag.CodeUnsupported, 0,
				"built-in %q expects exactly 1 positional argument", ref.Name)
		}
		if err := l.emit(fc.Args[0].Arg); err != nil {
			return err
		}
		l.push(op, 0)
		return nil
	}
	if ref.Name == "date" || ref.Name == "duration" {
		// Temporal literal: date("YYYY-MM-DD") / duration("P30D") -> a compile-time constant (ADR 0014).
		if len(fc.Args) != 1 || fc.Args[0].Name != "" {
			return diag.Newf(diag.CodeUnsupported, 0, "%s(...) expects one string literal, e.g. %s(\"...\")", ref.Name, ref.Name)
		}
		s, ok := fc.Args[0].Arg.(*feel.StringNode)
		if !ok {
			return diag.Newf(diag.CodeUnsupported, 0, "%s(...) requires a string literal argument", ref.Name)
		}
		var v ir.Value
		var err error
		if ref.Name == "date" {
			v, err = ir.ParseDate(s.Content())
		} else {
			v, err = ir.ParseDuration(s.Content())
		}
		if err != nil {
			return diag.Wrap(diag.CodeLiteral, 0, ref.Name+" literal", err)
		}
		l.push(ir.OpPushConst, l.constIdx(v))
		return nil
	}
	return l.emitBKMCall(fc)
}

// positionalArgs reports whether every argument is positional (no `name:` kwargs).
func positionalArgs(args []feel.FunCallArg) bool {
	for _, a := range args {
		if a.Name != "" {
			return false
		}
	}
	return true
}

// emitBKMCall inlines a BKM invocation `name(a1, ..., an)`: AST substitution of
// parameters by the argument nodes, then normal lowering of the substituted body.
func (l *lowerer) emitBKMCall(fc *feel.FunCall) error {
	ref, ok := fc.FunRef.(*feel.Var)
	if !ok {
		return diag.Newf(diag.CodeUnsupported, 0, "invocation: only `name(...)` is supported, got %s", fc.FunRef.Repr())
	}
	name := ref.Name
	bkm, ok := l.bkms[name]
	if !ok {
		return diag.Newf(diag.CodeUnsupported, 0, "invocation %q: unknown BKM", name).
			WithSuggestion("declare `bkm " + name + "(...)` or check the name")
	}
	// Recursion (self or mutual) detected STATICALLY: outright refusal. Acyclic nesting
	// `f(f(x))` is NOT recursive and remains allowed.
	if l.recursive[name] {
		return diag.Newf(diag.CodeUnsupported, 0,
			"BKM recursion forbidden: %q invokes itself (directly or in a cycle)", name)
	}
	if len(fc.Args) != len(bkm.Params) {
		return diag.Newf(diag.CodeArity, 0, "invocation %q: %d argument(s) for %d parameter(s)",
			name, len(fc.Args), len(bkm.Params))
	}
	// v1 scope: positional only (no kwargs `f(x: 1)`).
	subst := make(map[string]feel.Node, len(bkm.Params))
	for i, arg := range fc.Args {
		if arg.Name != "" {
			return diag.Newf(diag.CodeUnsupported, 0,
				"invocation %q: named arguments not supported (positional BKM only)", name)
		}
		subst[bkm.Params[i].Name] = arg.Arg
	}
	// `?` (column value) makes no sense in a BKM body: outright refusal.
	if hasColumnRef(bkm.Body.Node) {
		return diag.Newf(diag.CodeUnsupported, 0, "BKM %q: `?` (column value) forbidden in a BKM body", name)
	}
	// Depth guard (bounds RAM even for a pathological acyclic nesting).
	l.inlineDepth++
	if l.inlineDepth > maxInlineDepth {
		l.inlineDepth--
		return diag.Newf(diag.CodeUnsupported, 0,
			"excessive BKM inlining depth (> %d) on %q — nesting too deep", maxInlineDepth, name)
	}
	body := substitute(bkm.Body.Node, subst)
	err := l.emit(body)
	l.inlineDepth--
	return err
}

// substitute clones a FEEL node, replacing each *feel.Var{Name ∈ subst} by the
// corresponding argument node. Covers the lowerable subset; other types are returned
// as-is (lowering will reject them outright if unsupported).
func substitute(node feel.Node, subst map[string]feel.Node) feel.Node {
	switch n := node.(type) {
	case *feel.Var:
		if repl, ok := subst[n.Name]; ok {
			return repl
		}
		return n
	case *feel.Binop:
		return &feel.Binop{Op: n.Op, Left: substitute(n.Left, subst), Right: substitute(n.Right, subst)}
	case *feel.IfExpr:
		return &feel.IfExpr{
			Cond:       substitute(n.Cond, subst),
			ThenBranch: substitute(n.ThenBranch, subst),
			ElseBranch: substitute(n.ElseBranch, subst),
		}
	case *feel.FunCall:
		args := make([]feel.FunCallArg, len(n.Args))
		for i, a := range n.Args {
			args[i] = feel.FunCallArg{Name: a.Name, Arg: substitute(a.Arg, subst)}
		}
		return &feel.FunCall{FunRef: n.FunRef, Args: args}
	default:
		return node // literals and unsupported types: unchanged
	}
}

// hasColumnRef reports whether the expression references `?` (column value) in the lowerable
// subset. Used to forbid `?` in a BKM body (arguments themselves may contain `?`).
func hasColumnRef(node feel.Node) bool {
	switch n := node.(type) {
	case *feel.Var:
		return n.Name == "?"
	case *feel.Binop:
		return hasColumnRef(n.Left) || hasColumnRef(n.Right)
	case *feel.IfExpr:
		return hasColumnRef(n.Cond) || hasColumnRef(n.ThenBranch) || hasColumnRef(n.ElseBranch)
	case *feel.FunCall:
		for _, a := range n.Args {
			if hasColumnRef(a.Arg) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// progUsesInput reports whether a program references `?` (OpLoadInput) — forbidden outside a cell.
func progUsesInput(p *ir.ExprProgram) bool {
	for _, in := range p.Code {
		if in.Op == ir.OpLoadInput {
			return true
		}
	}
	return false
}

// stringPredOpcode maps a string-predicate built-in name to its opcode (caller guarantees the name).
func stringPredOpcode(name string) ir.Opcode {
	switch name {
	case "starts_with":
		return ir.OpStartsWith
	case "ends_with":
		return ir.OpEndsWith
	default: // "contains"
		return ir.OpContains
	}
}

func binopcode(op string) (ir.Opcode, error) {
	switch op {
	case "+":
		return ir.OpAdd, nil
	case "-":
		return ir.OpSub, nil
	case "*":
		return ir.OpMul, nil
	case "/":
		return ir.OpDivOp, nil
	case "=":
		return ir.OpEqOp, nil
	case "!=":
		return ir.OpNeOp, nil
	case "<":
		return ir.OpLtOp, nil
	case "<=":
		return ir.OpLeOp, nil
	case ">":
		return ir.OpGtOp, nil
	case ">=":
		return ir.OpGeOp, nil
	case "and":
		return ir.OpAnd, nil
	case "or":
		return ir.OpOr, nil
	default:
		return 0, diag.Newf(diag.CodeUnsupported, 0, "operator not supported in v2: %q", op)
	}
}

// maxStack computes an UPPER bound of the stack depth. With jumps (if/then/else),
// the linear pass overestimates (counts both branches) — this is safe: the VM stack is a slice
// that grows by append, MaxStack is only a capacity hint. Unary opcodes (not/floor/
// ceiling/round) are neutral; OpJmpFalse pops the condition.
func maxStack(code []ir.Instr) int {
	depth, max := 0, 0
	for _, in := range code {
		switch in.Op {
		case ir.OpPushConst, ir.OpLoadVar, ir.OpLoadInput:
			depth++
		case ir.OpAdd, ir.OpSub, ir.OpMul, ir.OpDivOp,
			ir.OpEqOp, ir.OpNeOp, ir.OpLtOp, ir.OpLeOp, ir.OpGtOp, ir.OpGeOp,
			ir.OpAnd, ir.OpOr, ir.OpRoundN, ir.OpMod, ir.OpPow,
			ir.OpStartsWith, ir.OpEndsWith, ir.OpContains, ir.OpJmpFalse:
			depth-- // pop 2 push 1 (binops/logical/two-arg builtins); OpJmpFalse pops the condition
		}
		if depth > max {
			max = depth
		}
	}
	return max
}
