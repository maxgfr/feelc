// Package fmtrules ré-émet une source .rules sous une forme CANONIQUE et IDEMPOTENTE, à partir
// du *model.Model (PAS de l'AST FEEL : feel.Node.Repr() produit des S-expressions non-FEEL qui
// ne reparsent pas). On réutilise les chaînes source verbatim (Cell.Src, Expr.Src, Input.Domain),
// que le parseur ne fait que TrimSpace — d'où l'idempotence : fmt(fmt(x)) == fmt(x).
//
// PERTES ASSUMÉES (le parseur ne les expose pas — jamais conformer en silence) :
//   - les COMMENTAIRES (`# ...`, y compris les en-têtes de colonnes) : retirés à la lecture ;
//   - le corps du bloc `model` (ex: `rounding: half_even`) : consommé, non stocké ;
//   - l'alignement/espacement d'origine et Cell.Col.
// Ces pertes sont documentées et verrouillées par un test ; `feelc fmt` les signale sur stderr.
package fmtrules

import (
	"fmt"
	"strings"

	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/model"
)

// Source parse puis re-formate une source .rules.
func Source(src string) (string, error) {
	m, err := dsl.Parse(src)
	if err != nil {
		return "", err
	}
	return Format(m), nil
}

// Format ré-émet un modèle sous forme canonique.
func Format(m *model.Model) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model %q {}\n", m.Name)

	if len(m.Inputs) > 0 {
		b.WriteString("\n")
		w := 0
		for _, in := range m.Inputs {
			if len(in.Name) > w {
				w = len(in.Name)
			}
		}
		for _, in := range m.Inputs {
			line := fmt.Sprintf("input %-*s : %s", w, in.Name, string(in.Type))
			if in.Domain != "" {
				line += " " + in.Domain
			}
			b.WriteString(strings.TrimRight(line, " ") + "\n")
		}
	}

	for _, td := range m.Types {
		b.WriteString("\n")
		fmt.Fprintf(&b, "type %s = context { ", td.Name)
		for i, f := range td.Fields {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(&b, "%s: %s", f.Name, string(f.Type))
		}
		b.WriteString(" }\n")
	}

	for _, bkm := range m.BKMs {
		b.WriteString("\n")
		params := make([]string, len(bkm.Params))
		for i, p := range bkm.Params {
			params[i] = fmt.Sprintf("%s: %s", p.Name, string(p.Type))
		}
		body := ""
		if bkm.Body != nil {
			body = bkm.Body.Src
		}
		fmt.Fprintf(&b, "bkm %s(%s): %s = %s\n", bkm.Name, strings.Join(params, ", "), string(bkm.Ret), body)
	}

	for _, d := range m.Decisions {
		b.WriteString("\n")
		writeDecision(&b, d)
	}
	return b.String()
}

func writeDecision(b *strings.Builder, d model.Decision) {
	if d.Expr != nil { // décision literal-expression
		fmt.Fprintf(b, "decision %s : %s = %s\n", d.Name, d.TypeName, d.Expr.Src)
		return
	}
	fmt.Fprintf(b, "decision %s : %s {\n", d.Name, d.TypeName)
	if len(d.Needs) > 0 {
		fmt.Fprintf(b, "  needs: %s\n", strings.Join(d.Needs, ", "))
	}
	fmt.Fprintf(b, "  hit: %s\n", d.HitPolicy)
	if len(d.Priority) > 0 {
		prio := make([]string, len(d.Priority))
		for i, c := range d.Priority {
			prio[i] = c.Src
		}
		fmt.Fprintf(b, "  priority: %s\n", strings.Join(prio, ", "))
	}

	nCond := len(d.Needs)
	// Largeur max par colonne (conditions et sorties), alignement déterministe -> idempotent.
	condW := make([]int, nCond)
	hasDefault := false
	var nOut int
	outW := []int{}
	for _, r := range d.Rules {
		if r.IsDefault {
			hasDefault = true
		}
		if len(r.Outputs) > nOut {
			nOut = len(r.Outputs)
			outW = make([]int, nOut)
		}
	}
	for _, r := range d.Rules {
		if !r.IsDefault {
			for j := 0; j < nCond && j < len(r.Conds); j++ {
				if w := len(r.Conds[j].Src); w > condW[j] {
					condW[j] = w
				}
			}
		}
		for k := 0; k < len(r.Outputs) && k < nOut; k++ {
			if w := len(r.Outputs[k].Src); w > outW[k] {
				outW[k] = w
			}
		}
	}
	if hasDefault && nCond > 0 && len("default") > condW[0] {
		condW[0] = len("default")
	}

	for _, r := range d.Rules {
		cells := make([]string, nCond)
		if r.IsDefault {
			for j := 0; j < nCond; j++ {
				if j == 0 {
					cells[0] = fmt.Sprintf("%-*s", condW[0], "default")
				} else {
					cells[j] = fmt.Sprintf("%-*s", condW[j], "")
				}
			}
		} else {
			for j := 0; j < nCond; j++ {
				src := ""
				if j < len(r.Conds) {
					src = r.Conds[j].Src
				}
				cells[j] = fmt.Sprintf("%-*s", condW[j], src)
			}
		}
		outs := make([]string, len(r.Outputs))
		for k, c := range r.Outputs {
			outs[k] = fmt.Sprintf("%-*s", outW[k], c.Src)
		}
		var line string
		if nCond > 0 {
			line = "  " + strings.Join(cells, " | ") + " => " + strings.Join(outs, " | ")
		} else {
			line = "  => " + strings.Join(outs, " | ")
		}
		b.WriteString(strings.TrimRight(line, " ") + "\n")
	}
	b.WriteString("}\n")
}
