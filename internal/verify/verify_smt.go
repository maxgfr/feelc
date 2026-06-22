//go:build smt

// SMT backend (Z3) — compiled ONLY with `-tags smt` (ADR 0007). smtProve branch: tables
// with Op=Prog cells (non-geometric), which the hyper-rectangle algebra cannot decide, are
// routed to Z3 for a completeness proof. HONEST degradation (never conform silently):
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
	query, ok := buildCompletenessQuery(cm, d)
	if !ok {
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: form not encodable in SMT (if/then/else, floor/ceiling/round, string column, or decision dependency)"})
		return true
	}
	z3, err := exec.LookPath("z3")
	if err != nil {
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: z3 not found in PATH (install z3 for the SMT proof)"})
		return true
	}
	out, runErr := runZ3(z3, query)
	switch {
	case runErr != nil:
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: z3 (SMT) execution failed: " + runErr.Error()})
	case strings.HasPrefix(out, "unsat"):
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevInfo,
			Message: "table with Op=Prog cells: completeness PROVEN by SMT (no gap) — residual cleared"})
	case strings.HasPrefix(out, "sat"):
		rep.add(Finding{Decision: d.Name, Kind: KindGap, Severity: SevError,
			Message: "completeness gap PROVEN by SMT (Op=Prog): there exists an input covered by no rule"})
	default:
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "Op=Prog residual: SMT undecided (" + strings.TrimSpace(out) + ")"})
	}
	return true
}

func runZ3(z3, query string) (string, error) {
	cmd := exec.Command(z3, "-in")
	cmd.Stdin = strings.NewReader(query)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
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
	vars := map[string]string{}
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
	resolve := func(n string) (string, bool) { v, ok := vars[n]; return v, ok }

	for _, col := range t.Inputs {
		if _, ok := vars[col]; !ok {
			return "", false // non-encodable column (string, or dependent decision)
		}
	}

	var asserts []string
	for _, n := range names {
		v, ok := vars[n]
		if !ok {
			continue
		}
		if c, ok := domainSMT(cm.Domains[n], v); ok && c != "" {
			asserts = append(asserts, c)
		}
	}
	notRules := make([]string, 0, len(t.Rules))
	for _, r := range t.Rules {
		cells := make([]string, 0, len(r.Conds))
		for j, ct := range r.Conds {
			s, ok := smt.Cell(ct, vars[t.Inputs[j]], resolve)
			if !ok {
				return "", false
			}
			cells = append(cells, s)
		}
		rule := "true"
		if len(cells) > 0 {
			rule = "(and " + strings.Join(cells, " ") + ")"
		}
		notRules = append(notRules, "(not "+rule+")")
	}

	var b strings.Builder
	b.WriteString("(set-logic QF_NRA)\n")
	b.WriteString(decls.String())
	for _, a := range asserts {
		b.WriteString("(assert " + a + ")\n")
	}
	if len(notRules) > 0 {
		b.WriteString("(assert (and " + strings.Join(notRules, " ") + "))\n")
	}
	b.WriteString("(check-sat)\n")
	return b.String(), true
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
