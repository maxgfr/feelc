package dmnxml

import (
	"fmt"
	"strings"

	"github.com/maxgfr/feelc/internal/model"
)

// Export is the inverse of Import: a *model.Model -> DMN XML (OUTPUT interop). Reconstructed
// from the verbatim source strings (Cell.Src, Expr.Src). Returns warnings for everything
// the DMN standard does NOT capture (never conform silently):
//   - input domains (`in [..]`, `>= 0`): outside the DMN decisionTable;
//   - `priority:` list: DMN PRIORITY relies on the order of output values, not expressed here;
//   - BKM: inlined at compile time, not mapped to a DMN businessKnowledgeModel;
//   - `default` line: emitted as an "all any" rule (DMN has no default keyword).
func Export(m *model.Model) ([]byte, []string, error) {
	var warns []string
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&b, `<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/" name=%s>`+"\n", xmlAttr(m.Name))

	for _, in := range m.Inputs {
		fmt.Fprintf(&b, "  <inputData name=%s><variable typeRef=%s/></inputData>\n", xmlAttr(in.Name), xmlAttr(string(in.Type)))
		if in.Domain != "" {
			warns = append(warns, fmt.Sprintf("input %q: domain %q not exported (outside the DMN decisionTable)", in.Name, in.Domain))
		}
	}
	for _, bkm := range m.BKMs {
		warns = append(warns, fmt.Sprintf("BKM %q not exported (inlined at compile time, no DMN mapping)", bkm.Name))
	}
	for _, d := range m.Decisions {
		writeDecisionXML(&b, m, d, &warns)
	}
	b.WriteString("</definitions>\n")
	return []byte(b.String()), warns, nil
}

func writeDecisionXML(b *strings.Builder, m *model.Model, d model.Decision, warns *[]string) {
	if d.Expr != nil {
		fmt.Fprintf(b, "  <decision name=%s>\n", xmlAttr(d.Name))
		fmt.Fprintf(b, "    <variable typeRef=%s/>\n", xmlAttr(d.TypeName))
		fmt.Fprintf(b, "    <literalExpression><text>%s</text></literalExpression>\n", xmlEsc(d.Expr.Src))
		b.WriteString("  </decision>\n")
		return
	}

	hp, agg := mapHitPolicyOut(d.HitPolicy)
	if hp == "PRIORITY" && len(d.Priority) > 0 {
		*warns = append(*warns, fmt.Sprintf("decision %q: the `priority:` line is not expressible in standard DMN — lost on export", d.Name))
	}
	for _, r := range d.Rules {
		if r.IsDefault {
			*warns = append(*warns, fmt.Sprintf("decision %q: `default` line emitted as an \"all any\" rule (DMN has no default keyword) — semantics approximated", d.Name))
			break
		}
	}
	fmt.Fprintf(b, "  <decision name=%s>\n", xmlAttr(d.Name))
	outs := outputCols(m, d)
	if len(outs) == 1 {
		fmt.Fprintf(b, "    <variable typeRef=%s/>\n", xmlAttr(outs[0].typ))
	} else {
		fmt.Fprintf(b, "    <variable typeRef=%s/>\n", xmlAttr(d.TypeName))
	}
	if agg != "" {
		fmt.Fprintf(b, "    <decisionTable hitPolicy=%s aggregation=%s>\n", xmlAttr(hp), xmlAttr(agg))
	} else {
		fmt.Fprintf(b, "    <decisionTable hitPolicy=%s>\n", xmlAttr(hp))
	}
	for _, n := range d.Needs {
		fmt.Fprintf(b, "      <input><inputExpression typeRef=%s><text>%s</text></inputExpression></input>\n",
			xmlAttr(inputType(m, n)), xmlEsc(n))
	}
	for _, o := range outs {
		fmt.Fprintf(b, "      <output name=%s typeRef=%s/>\n", xmlAttr(o.name), xmlAttr(o.typ))
	}
	for _, r := range d.Rules {
		b.WriteString("      <rule>\n")
		for j := 0; j < len(d.Needs); j++ {
			cell := "" // `default` or absent column -> empty DMN entry (= any)
			if !r.IsDefault && j < len(r.Conds) && !r.Conds[j].Dash {
				cell = r.Conds[j].Src
			}
			fmt.Fprintf(b, "        <inputEntry><text>%s</text></inputEntry>\n", xmlEsc(cell))
		}
		for _, o := range r.Outputs {
			fmt.Fprintf(b, "        <outputEntry><text>%s</text></outputEntry>\n", xmlEsc(o.Src))
		}
		b.WriteString("      </rule>\n")
	}
	b.WriteString("    </decisionTable>\n")
	b.WriteString("  </decision>\n")
}

type outCol struct{ name, typ string }

// outputCols infers the output columns: scalar -> 1 (name = decision); context -> fields.
func outputCols(m *model.Model, d model.Decision) []outCol {
	switch model.Type(d.TypeName) {
	case model.TypeNumber, model.TypeString, model.TypeBool:
		return []outCol{{name: d.Name, typ: d.TypeName}}
	}
	if td, ok := m.Type(d.TypeName); ok {
		cols := make([]outCol, len(td.Fields))
		for i, f := range td.Fields {
			cols[i] = outCol{name: f.Name, typ: string(f.Type)}
		}
		return cols
	}
	return []outCol{{name: d.Name, typ: "string"}}
}

// inputType: DMN type of a `needs` column (type of the referenced input, else decision, else number).
func inputType(m *model.Model, name string) string {
	for _, in := range m.Inputs {
		if in.Name == name {
			return string(in.Type)
		}
	}
	for _, d := range m.Decisions {
		if d.Name == name && model.Type(d.TypeName) != "" {
			switch model.Type(d.TypeName) {
			case model.TypeNumber, model.TypeString, model.TypeBool:
				return d.TypeName
			}
		}
	}
	return "number"
}

func mapHitPolicyOut(hp string) (policy, agg string) {
	switch strings.TrimSpace(hp) {
	case "first":
		return "FIRST", ""
	case "unique", "":
		return "UNIQUE", ""
	case "any":
		return "ANY", ""
	case "priority":
		return "PRIORITY", ""
	case "rule order":
		return "RULE ORDER", ""
	case "output order":
		return "OUTPUT ORDER", ""
	case "collect":
		return "COLLECT", ""
	case "collect sum":
		return "COLLECT", "SUM"
	case "collect min":
		return "COLLECT", "MIN"
	case "collect max":
		return "COLLECT", "MAX"
	case "collect count":
		return "COLLECT", "COUNT"
	default:
		return "UNIQUE", ""
	}
}

// xmlEsc escapes text for XML content (cells may contain <, >, &, ").
func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// xmlAttr renders an escaped, quoted XML attribute value (and NOT via %q, which applies
// Go escaping, not XML).
func xmlAttr(s string) string { return `"` + xmlEsc(s) + `"` }
