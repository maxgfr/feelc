//go:build smt

// Backend SMT (Z3) — compilé UNIQUEMENT avec `-tags smt` (ADR 0007). Branche smtProve : les tables
// à cellules Op=Prog (non géométriques), que l'algèbre d'hyper-rectangles ne peut décider, sont
// routées vers Z3 pour une preuve de complétude. Dégradation HONNÊTE (jamais conformer en silence) :
// z3 absent du PATH, ou forme hors sous-ensemble encodable → `not-verifiable` avec la raison.
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
			Message: "résidu Op=Prog : forme non encodable en SMT (if/then/else, floor/ceiling/round, colonne string, ou dépendance décision)"})
		return true
	}
	z3, err := exec.LookPath("z3")
	if err != nil {
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "résidu Op=Prog : z3 introuvable dans le PATH (installez z3 pour la preuve SMT)"})
		return true
	}
	out, runErr := runZ3(z3, query)
	switch {
	case runErr != nil:
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "résidu Op=Prog : exécution z3 (SMT) échouée: " + runErr.Error()})
	case strings.HasPrefix(out, "unsat"):
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevInfo,
			Message: "table à cellules Op=Prog : complétude PROUVÉE par SMT (aucun trou) — résidu levé"})
	case strings.HasPrefix(out, "sat"):
		rep.add(Finding{Decision: d.Name, Kind: KindGap, Severity: SevError,
			Message: "trou de complétude PROUVÉ par SMT (Op=Prog) : il existe une entrée couverte par aucune règle"})
	default:
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "résidu Op=Prog : SMT indécis (" + strings.TrimSpace(out) + ")"})
	}
	return true
}

func runZ3(z3, query string) (string, error) {
	cmd := exec.Command(z3, "-in")
	cmd.Stdin = strings.NewReader(query)
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// buildCompletenessQuery encode « il existe une entrée dans le domaine couverte par AUCUNE règle »
// (unsat ⇒ table complète). Seulement pour les politiques single-hit (la complétude n'a de sens
// que là). ok=false si une cellule/colonne est hors sous-ensemble encodable.
func buildCompletenessQuery(cm *ir.CompiledModel, d *ir.Decision) (string, bool) {
	t := d.Table
	switch t.HitPolicy {
	case ir.HitFirst, ir.HitUnique, ir.HitAny, ir.HitPriority:
	default:
		return "", false // COLLECT / RULE ORDER : pas de notion de trou
	}
	vars := map[string]string{}
	names := make([]string, 0, len(cm.Inputs))
	for n := range cm.Inputs {
		names = append(names, n)
	}
	sort.Strings(names) // déterminisme du script
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
			return "", false // colonne non encodable (string, ou décision dépendante)
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
