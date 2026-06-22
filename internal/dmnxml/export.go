package dmnxml

import (
	"fmt"
	"strings"

	"github.com/maxgfr/feelc/internal/model"
)

// Export est l'inverse de Import : un *model.Model -> DMN XML (interop de SORTIE). Reconstruit
// depuis les chaînes source verbatim (Cell.Src, Expr.Src). Renvoie des avertissements pour tout
// ce que le DMN standard ne capture PAS (jamais conformer en silence) :
//   - domaines d'entrée (`in [..]`, `>= 0`) : hors decisionTable DMN ;
//   - liste `priority:` : la PRIORITY DMN repose sur l'ordre des valeurs de sortie, non exprimé ici ;
//   - BKM : inliné à la compilation, non mappé vers un businessKnowledgeModel DMN ;
//   - ligne `default` : émise comme une règle « tout any » (DMN n'a pas de mot-clé default).
func Export(m *model.Model) ([]byte, []string, error) {
	var warns []string
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	fmt.Fprintf(&b, `<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/" name=%s>`+"\n", xmlAttr(m.Name))

	for _, in := range m.Inputs {
		fmt.Fprintf(&b, "  <inputData name=%s><variable typeRef=%s/></inputData>\n", xmlAttr(in.Name), xmlAttr(string(in.Type)))
		if in.Domain != "" {
			warns = append(warns, fmt.Sprintf("input %q: domaine %q non exporté (hors decisionTable DMN)", in.Name, in.Domain))
		}
	}
	for _, bkm := range m.BKMs {
		warns = append(warns, fmt.Sprintf("BKM %q non exporté (inliné à la compilation, pas de mapping DMN)", bkm.Name))
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
		*warns = append(*warns, fmt.Sprintf("décision %q: la ligne `priority:` n'est pas exprimable en DMN standard — perdue à l'export", d.Name))
	}
	for _, r := range d.Rules {
		if r.IsDefault {
			*warns = append(*warns, fmt.Sprintf("décision %q: ligne `default` émise comme règle « tout any » (DMN n'a pas de mot-clé default) — sémantique approximée", d.Name))
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
			cell := "" // `default` ou colonne absente -> entrée vide DMN (= any)
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

// outputCols déduit les colonnes de sortie : scalaire -> 1 (nom = décision) ; context -> champs.
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

// inputType : type DMN d'une colonne `needs` (type de l'input référencé, sinon décision, sinon number).
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

// xmlEsc échappe le texte pour un contenu XML (les cellules peuvent contenir <, >, &, ").
func xmlEsc(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;", "'", "&apos;")
	return r.Replace(s)
}

// xmlAttr rend une valeur d'attribut XML échappée et entre guillemets (et NON via %q, qui applique
// l'échappement Go, pas XML).
func xmlAttr(s string) string { return `"` + xmlEsc(s) + `"` }
