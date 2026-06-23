//go:build smt

// SMT backend (Z3) — compiled ONLY with `-tags smt` (ADR 0007). smtProve branch: tables
// with Op=Prog cells (non-geometric), which the hyper-rectangle algebra cannot decide, are
// routed to Z3 for completeness AND conflict proofs. HONEST degradation (never conform silently):
// z3 absent from PATH, or a form outside the encodable subset → `not-verifiable` with the reason.
package verify

import (
	"os/exec"
	"sort"
	"strings"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/smt"
)

func init() { smtProve = proveSMT }

func proveSMT(cm *ir.CompiledModel, d *ir.Decision, rep *Report) bool {
	// Build both queries first (PURE, no solver). Completeness covers single-hit policies;
	// conflict covers UNIQUE/ANY. A form outside the encodable subset yields ok=false for both.
	cq, cok := buildCompletenessQuery(cm, d)
	xq, xok := buildConflictQuery(cm, d)
	if !cok && !xok {
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: form not encodable in SMT (string column, decision dependency, or unsupported construct)"})
		return true
	}
	z3, err := exec.LookPath("z3")
	if err != nil {
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: z3 not found in PATH (install z3 for the SMT proof)"})
		return true
	}
	if cok {
		runQuery(z3, cq, rep, d.Name, queryCompleteness)
	}
	if xok {
		runQuery(z3, xq, rep, d.Name, queryConflict)
	}
	return true
}

type queryKind int

const (
	queryCompleteness queryKind = iota
	queryConflict
)

// runQuery runs a single SMT script through z3 and turns the verdict into a Finding. The encoding
// is built so that `sat` is the defect (a gap, resp. a conflict) and `unsat` is the proof.
func runQuery(z3, query string, rep *Report, dName string, kind queryKind) {
	out, runErr := runZ3(z3, query)
	switch {
	case runErr != nil:
		rep.add(Finding{Decision: dName, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: z3 (SMT) execution failed: " + runErr.Error()})
	case strings.HasPrefix(out, "unsat"):
		if kind == queryCompleteness {
			rep.add(Finding{Decision: dName, Kind: KindNotVerifiable, Severity: SevInfo,
				Message: "table with Op=Prog cells: completeness PROVEN by SMT (no gap) — residual cleared"})
		} else {
			rep.add(Finding{Decision: dName, Kind: KindNotVerifiable, Severity: SevInfo,
				Message: "table with Op=Prog cells: consistency PROVEN by SMT (no conflict) — residual cleared"})
		}
	case strings.HasPrefix(out, "sat"):
		if kind == queryCompleteness {
			rep.add(Finding{Decision: dName, Kind: KindGap, Severity: SevError,
				Message: "completeness gap PROVEN by SMT (Op=Prog): there exists an input covered by no rule"})
		} else {
			rep.add(Finding{Decision: dName, Kind: KindConflict, Severity: SevError,
				Message: "conflict PROVEN by SMT (Op=Prog): there exists an input matched by overlapping rules"})
		}
	default:
		rep.add(Finding{Decision: dName, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: SMT undecided (" + strings.TrimSpace(out) + ")"})
	}
}

func runZ3(z3, query string) (string, error) {
	cmd := exec.Command(z3, "-in")
	cmd.Stdin = strings.NewReader(query)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// smtVars declares an SMT constant per encodable input (Real/Bool), checks that every column the
// table reads is encodable, and returns the declaration block + domain constraints. ok=false if a
// read column is non-encodable (string, or a dependent decision).
func smtVars(cm *ir.CompiledModel, t *ir.DecisionTable) (vars map[string]string, declBlock string, domAsserts []string, ok bool) {
	vars = map[string]string{}
	names := make([]string, 0, len(cm.Inputs))
	for n := range cm.Inputs {
		names = append(names, n)
	}
	sort.Strings(names) // script determinism
	var decls strings.Builder
	for _, n := range names {
		switch cm.Inputs[n] {
		case ir.TypeNumber:
			vars[n] = "v_" + n
			decls.WriteString("(declare-const v_" + n + " Real)\n")
		case ir.TypeBool:
			vars[n] = "v_" + n
			decls.WriteString("(declare-const v_" + n + " Bool)\n")
		}
	}
	for _, col := range t.Inputs {
		if _, ok := vars[col]; !ok {
			return nil, "", nil, false // non-encodable column (string, or dependent decision)
		}
	}
	for _, n := range names {
		v, ok := vars[n]
		if !ok {
			continue
		}
		if c, ok := domainSMT(cm.Domains[n], v); ok && c != "" {
			domAsserts = append(domAsserts, c)
		}
	}
	return vars, decls.String(), domAsserts, true
}

func resolverFor(vars map[string]string) smt.VarResolver {
	return func(n string) (string, bool) { v, ok := vars[n]; return v, ok }
}

// encodeRule encodes a rule's match condition as a single SMT boolean (`(and cell …)`, or `true`
// for an unconditional rule). The shared aux sink collects any round side-constraints.
func encodeRule(t *ir.DecisionTable, r ir.Rule, vars map[string]string, resolve smt.VarResolver, aux *smt.Aux) (string, bool) {
	cells := make([]string, 0, len(r.Conds))
	for j, ct := range r.Conds {
		s, ok := smt.Cell(ct, vars[t.Inputs[j]], resolve, aux)
		if !ok {
			return "", false
		}
		cells = append(cells, s)
	}
	if len(cells) == 0 {
		return "true", true
	}
	return "(and " + strings.Join(cells, " ") + ")", true
}

// assemble emits a full SMT script: logic, declarations (inputs + aux), domain + aux assertions,
// then the supplied body assertion, then check-sat. set-logic ALL admits the mixed Real/Int/mod
// terms introduced by floor/ceiling/round (the script text stays fully deterministic).
func assemble(declBlock string, aux *smt.Aux, domAsserts []string, body string) string {
	var b strings.Builder
	b.WriteString("(set-logic ALL)\n")
	b.WriteString(declBlock)
	for _, dcl := range aux.Decls {
		b.WriteString(dcl + "\n")
	}
	for _, a := range domAsserts {
		b.WriteString("(assert " + a + ")\n")
	}
	for _, a := range aux.Asserts {
		b.WriteString("(assert " + a + ")\n")
	}
	if body != "" {
		b.WriteString("(assert " + body + ")\n")
	}
	b.WriteString("(check-sat)\n")
	return b.String()
}

// buildCompletenessQuery encodes "there exists an input in the domain covered by NO rule"
// (unsat ⇒ complete table). Only for single-hit policies (completeness only makes sense
// there). ok=false if a cell/column is outside the encodable subset.
func buildCompletenessQuery(cm *ir.CompiledModel, d *ir.Decision) (string, bool) {
	t := d.Table
	switch t.HitPolicy {
	case ir.HitFirst, ir.HitUnique, ir.HitAny, ir.HitPriority:
	default:
		return "", false // COLLECT / RULE ORDER: no notion of a gap
	}
	vars, declBlock, domAsserts, ok := smtVars(cm, t)
	if !ok {
		return "", false
	}
	resolve := resolverFor(vars)
	aux := &smt.Aux{}
	notRules := make([]string, 0, len(t.Rules))
	for _, r := range t.Rules {
		rule, ok := encodeRule(t, r, vars, resolve, aux)
		if !ok {
			return "", false
		}
		notRules = append(notRules, "(not "+rule+")")
	}
	body := ""
	if len(notRules) > 0 {
		body = "(and " + strings.Join(notRules, " ") + ")"
	}
	return assemble(declBlock, aux, domAsserts, body), true
}

// buildConflictQuery encodes "there exists an input matched by two CONFLICTING rules" (sat ⇒
// conflict). Mirrors the geometric layer's recordConflict semantics: UNIQUE → any overlap is a
// conflict; ANY → only overlaps with divergent outputs. ok=false for other policies, a
// non-encodable form, or when no conflicting pair exists (nothing to prove).
func buildConflictQuery(cm *ir.CompiledModel, d *ir.Decision) (string, bool) {
	t := d.Table
	switch t.HitPolicy {
	case ir.HitUnique, ir.HitAny:
	default:
		return "", false
	}
	vars, declBlock, domAsserts, ok := smtVars(cm, t)
	if !ok {
		return "", false
	}
	resolve := resolverFor(vars)
	aux := &smt.Aux{}
	matches := make([]string, len(t.Rules))
	for i, r := range t.Rules {
		m, ok := encodeRule(t, r, vars, resolve, aux)
		if !ok {
			return "", false
		}
		matches[i] = m
	}
	var pairs []string
	for i := 0; i < len(t.Rules); i++ {
		for j := i + 1; j < len(t.Rules); j++ {
			conflicting := t.HitPolicy == ir.HitUnique ||
				!outputsEqual(t.Rules[i].Outputs, t.Rules[j].Outputs) // HitAny: only divergent outputs
			if conflicting {
				pairs = append(pairs, "(and "+matches[i]+" "+matches[j]+")")
			}
		}
	}
	if len(pairs) == 0 {
		return "", false // no candidate pair → no conflict possible
	}
	body := pairs[0]
	if len(pairs) > 1 {
		body = "(or " + strings.Join(pairs, " ") + ")"
	}
	return assemble(declBlock, aux, domAsserts, body), true
}

func domainSMT(dom ir.Domain, v string) (string, bool) {
	switch dom.Kind {
	case ir.DomNumeric:
		var parts []string
		if !dom.LoInf {
			lit, ok := smt.Literal(dom.Lo)
			if !ok {
				return "", false
			}
			op := ">="
			if dom.LoOpen {
				op = ">"
			}
			parts = append(parts, "("+op+" "+v+" "+lit+")")
		}
		if !dom.HiInf {
			lit, ok := smt.Literal(dom.Hi)
			if !ok {
				return "", false
			}
			op := "<="
			if dom.HiOpen {
				op = "<"
			}
			parts = append(parts, "("+op+" "+v+" "+lit+")")
		}
		if len(parts) == 0 {
			return "", true
		}
		return "(and " + strings.Join(parts, " ") + ")", true
	case ir.DomEnum:
		var parts []string
		for _, e := range dom.Enum {
			lit, ok := smt.Literal(e)
			if !ok {
				return "", false
			}
			parts = append(parts, "(= "+v+" "+lit+")")
		}
		if len(parts) == 0 {
			return "", true
		}
		return "(or " + strings.Join(parts, " ") + ")", true
	default:
		return "", true
	}
}
