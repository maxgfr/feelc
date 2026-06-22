// Package dmnxml importe un modèle DMN XML (Camunda/Drools) vers le DSL .rules de feelc.
// Import ONE-WAY (interop d'entrée) : on tolère et on SIGNALE les constructs hors sous-ensemble
// plutôt que d'échouer durement. La syntaxe des cellules DMN (unary tests FEEL) est conservée
// telle quelle (feelc partage la même sémantique).
package dmnxml

import (
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

type definitions struct {
	XMLName   xml.Name    `xml:"definitions"`
	Name      string      `xml:"name,attr"`
	InputData []inputData `xml:"inputData"`
	Decisions []decision  `xml:"decision"`
}

type inputData struct {
	Name     string   `xml:"name,attr"`
	Variable variable `xml:"variable"`
}

type variable struct {
	TypeRef string `xml:"typeRef,attr"`
}

type decision struct {
	Name     string         `xml:"name,attr"`
	Variable variable       `xml:"variable"`
	Table    *decisionTable `xml:"decisionTable"`
	Literal  *literalExpr   `xml:"literalExpression"`
}

type decisionTable struct {
	HitPolicy   string      `xml:"hitPolicy,attr"`
	Aggregation string      `xml:"aggregation,attr"`
	Inputs      []dmnInput  `xml:"input"`
	Outputs     []dmnOutput `xml:"output"`
	Rules       []dmnRule   `xml:"rule"`
}

type dmnInput struct {
	Expression struct {
		TypeRef string `xml:"typeRef,attr"`
		Text    string `xml:"text"`
	} `xml:"inputExpression"`
}

type dmnOutput struct {
	Name    string `xml:"name,attr"`
	Label   string `xml:"label,attr"`
	TypeRef string `xml:"typeRef,attr"`
}

type dmnRule struct {
	InputEntries  []string `xml:"inputEntry>text"`
	OutputEntries []string `xml:"outputEntry>text"`
}

type literalExpr struct {
	Text string `xml:"text"`
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Import convertit un DMN XML en source .rules. Renvoie aussi des avertissements (constructs
// hors sous-ensemble, simplifications).
func Import(data []byte) (string, []string, error) {
	var def definitions
	if err := xml.Unmarshal(data, &def); err != nil {
		return "", nil, fmt.Errorf("XML DMN invalide: %w", err)
	}
	var warns []string
	var b strings.Builder
	name := def.Name
	if name == "" {
		name = "imported"
	}
	fmt.Fprintf(&b, "model %q {}\n\n", name)

	for _, in := range def.InputData {
		fmt.Fprintf(&b, "input %s : %s\n", in.Name, mapType(in.Variable.TypeRef, &warns))
	}
	b.WriteString("\n")

	for _, d := range def.Decisions {
		switch {
		case d.Literal != nil:
			fmt.Fprintf(&b, "decision %s : %s = %s\n\n", d.Name, mapType(d.Variable.TypeRef, &warns), strings.TrimSpace(d.Literal.Text))
		case d.Table != nil:
			writeTable(&b, d, &warns)
		default:
			warns = append(warns, fmt.Sprintf("décision %q: ni table ni expression littérale — ignorée", d.Name))
		}
	}
	return b.String(), warns, nil
}

func writeTable(b *strings.Builder, d decision, warns *[]string) {
	t := d.Table
	needs := make([]string, len(t.Inputs))
	for i, in := range t.Inputs {
		expr := strings.TrimSpace(in.Expression.Text)
		needs[i] = expr
		if !identRe.MatchString(expr) {
			*warns = append(*warns, fmt.Sprintf("décision %q: inputExpression %q n'est pas un simple identifiant — `needs` invalide, à corriger", d.Name, expr))
		}
	}

	outType := "string"
	if len(t.Outputs) == 1 {
		outType = mapType(t.Outputs[0].TypeRef, warns)
	} else if len(t.Outputs) > 1 {
		// Sortie multi-colonnes -> type context dédié.
		tn := d.Name + "Result"
		fmt.Fprintf(b, "type %s = context { ", tn)
		for i, o := range t.Outputs {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%s: %s", outName(o, i), mapType(o.TypeRef, warns))
		}
		b.WriteString(" }\n")
		outType = tn
	}

	hit := mapHitPolicy(t.HitPolicy, t.Aggregation, d.Name, warns)
	fmt.Fprintf(b, "decision %s : %s {\n", d.Name, outType)
	fmt.Fprintf(b, "  needs: %s\n", strings.Join(needs, ", "))
	fmt.Fprintf(b, "  hit: %s\n", hit)
	for _, r := range t.Rules {
		conds := make([]string, len(r.InputEntries))
		for i, e := range r.InputEntries {
			e = strings.TrimSpace(e)
			if e == "" {
				e = "-" // entrée vide DMN = any
			}
			conds[i] = e
		}
		outs := make([]string, len(r.OutputEntries))
		for i, e := range r.OutputEntries {
			outs[i] = strings.TrimSpace(e)
		}
		fmt.Fprintf(b, "  %s => %s\n", strings.Join(conds, " | "), strings.Join(outs, " | "))
	}
	b.WriteString("}\n\n")
}

func outName(o dmnOutput, i int) string {
	if o.Name != "" {
		return o.Name
	}
	if identRe.MatchString(o.Label) {
		return o.Label
	}
	return fmt.Sprintf("o%d", i+1)
}

func mapType(t string, warns *[]string) string {
	switch strings.ToLower(strings.TrimSpace(t)) {
	case "number", "integer", "int", "long", "double", "decimal":
		return "number"
	case "string", "":
		if t == "" {
			return "string"
		}
		return "string"
	case "boolean", "bool":
		return "boolean"
	default:
		*warns = append(*warns, fmt.Sprintf("typeRef DMN inconnu %q -> traité comme string", t))
		return "string"
	}
}

func mapHitPolicy(hp, agg, dec string, warns *[]string) string {
	switch strings.ToUpper(strings.TrimSpace(hp)) {
	case "", "UNIQUE":
		return "unique" // défaut DMN
	case "ANY":
		return "any"
	case "FIRST":
		return "first"
	case "RULE ORDER":
		return "rule order"
	case "PRIORITY":
		*warns = append(*warns, fmt.Sprintf("décision %q: PRIORITY importé en FIRST — ajoutez une ligne `priority:` si besoin", dec))
		return "first"
	case "COLLECT":
		switch strings.ToUpper(strings.TrimSpace(agg)) {
		case "", "LIST":
			return "collect"
		case "SUM":
			return "collect sum"
		case "MIN":
			return "collect min"
		case "MAX":
			return "collect max"
		case "COUNT":
			return "collect count"
		default:
			*warns = append(*warns, fmt.Sprintf("décision %q: agrégation COLLECT %q inconnue -> collect", dec, agg))
			return "collect"
		}
	default:
		*warns = append(*warns, fmt.Sprintf("décision %q: hit policy DMN %q non supportée -> unique", dec, hp))
		return "unique"
	}
}
