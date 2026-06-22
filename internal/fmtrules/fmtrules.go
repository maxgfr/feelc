// Package fmtrules re-emits a .rules source in a CANONICAL and IDEMPOTENT form, from
// the *model.Model (NOT the FEEL AST: feel.Node.Repr() produces non-FEEL S-expressions that
// do not reparse). We reuse the verbatim source strings (Cell.Src, Expr.Src, Input.Domain),
// which the parser only TrimSpaces — hence idempotence: fmt(fmt(x)) == fmt(x).
//
// ACCEPTED LOSSES (the parser does not expose them — never conform in silence):
//   - COMMENTS (`# ...`, including column headers): dropped on read;
//   - the body of the `model` block (e.g. `rounding: half_even`): consumed, not stored;
//   - the original alignment/spacing and Cell.Col.
//
// These losses are documented and locked by a test; `feelc fmt` reports them on stderr.
package fmtrules

import (
	"fmt"
	"strings"

	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/model"
)

// Source parses then re-formats a .rules source.
func Source(src string) (string, error) {
	m, err := dsl.Parse(src)
	if err != nil {
		return "", err
	}
	return Format(m), nil
}

// Format re-emits a model in canonical form.
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
	if d.Expr != nil { // literal-expression decision
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
	// Max width per column (conditions and outputs), deterministic alignment -> idempotent.
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
