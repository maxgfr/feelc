// Package dmnxml imports a DMN XML model (Camunda/Drools) into feelc's .rules DSL.
// ONE-WAY import (input interop): out-of-subset constructs are tolerated and REPORTED
// rather than causing a hard failure. The syntax of DMN cells (FEEL unary tests) is kept
// as-is (feelc shares the same semantics).
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
	// OutputValues declares the allowed output values; for PRIORITY / OUTPUT ORDER they are listed
	// in decreasing priority order (DMN §8.2.11) and define the resolution ordering.
	OutputValues struct {
		Text string `xml:"text"`
	} `xml:"outputValues"`
}

type dmnRule struct {
	InputEntries  []string `xml:"inputEntry>text"`
	OutputEntries []string `xml:"outputEntry>text"`
}

type literalExpr struct {
	Text string `xml:"text"`
}

var identRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Import converts a DMN XML into .rules source. It also returns warnings (out-of-subset
// constructs, simplifications).
func Import(data []byte) (string, []string, error) {
	var def definitions
	if err := xml.Unmarshal(data, &def); err != nil {
		return "", nil, fmt.Errorf("invalid DMN XML: %w", err)
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
			warns = append(warns, fmt.Sprintf("decision %q: neither table nor literal expression — ignored", d.Name))
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
			*warns = append(*warns, fmt.Sprintf("decision %q: inputExpression %q is not a simple identifier — invalid `needs`, to be fixed", d.Name, expr))
		}
	}

	outType := "string"
	if len(t.Outputs) == 1 {
		outType = mapType(t.Outputs[0].TypeRef, warns)
	} else if len(t.Outputs) > 1 {
		// Multi-column output -> dedicated context type.
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
	// PRIORITY / OUTPUT ORDER rank outputs by the declared <outputValues> (decreasing priority).
	if hit == "priority" || hit == "output order" {
		if pr := priorityLine(t.Outputs); pr != "" {
			fmt.Fprintf(b, "  priority: %s\n", pr)
		} else {
			*warns = append(*warns, fmt.Sprintf("decision %q: %s needs a priority order but no <outputValues> were found — add a `priority:` line manually", d.Name, strings.ToUpper(hit)))
		}
	}
	for _, r := range t.Rules {
		conds := make([]string, len(r.InputEntries))
		for i, e := range r.InputEntries {
			e = strings.TrimSpace(e)
			if e == "" {
				e = "-" // empty DMN entry = any
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

// priorityLine returns the comma-separated FEEL output values (decreasing priority) declared on the
// ranked output's <outputValues>, suitable for a DSL `priority:` line. Empty if none are declared.
func priorityLine(outs []dmnOutput) string {
	if len(outs) == 0 {
		return ""
	}
	return strings.TrimSpace(outs[0].OutputValues.Text)
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
	key := strings.ToLower(strings.TrimSpace(t))
	if i := strings.LastIndexByte(key, ':'); i >= 0 {
		key = key[i+1:] // strip a namespace prefix (xsd:date, feel:date, …)
	}
	switch key {
	case "number", "integer", "int", "long", "double", "decimal":
		return "number"
	case "string", "":
		if t == "" {
			return "string"
		}
		return "string"
	case "boolean", "bool":
		return "boolean"
	case "date":
		return "date"
	case "daystimeduration", "daytimeduration", "duration":
		return "duration"
	default:
		*warns = append(*warns, fmt.Sprintf("unknown DMN typeRef %q -> treated as string", t))
		return "string"
	}
}

func mapHitPolicy(hp, agg, dec string, warns *[]string) string {
	switch strings.ToUpper(strings.TrimSpace(hp)) {
	case "", "UNIQUE":
		return "unique" // DMN default
	case "ANY":
		return "any"
	case "FIRST":
		return "first"
	case "RULE ORDER":
		return "rule order"
	case "PRIORITY":
		return "priority"
	case "OUTPUT ORDER":
		return "output order"
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
			*warns = append(*warns, fmt.Sprintf("decision %q: unknown COLLECT aggregation %q -> collect", dec, agg))
			return "collect"
		}
	default:
		*warns = append(*warns, fmt.Sprintf("decision %q: unsupported hit policy DMN %q -> unique", dec, hp))
		return "unique"
	}
}
