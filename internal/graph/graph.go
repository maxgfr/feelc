// Package graph builds the Decision Requirements Graph (DRG) of a compiled model — inputs and
// decisions as nodes, information requirements (Decision.Deps) as edges — and renders it to DOT,
// Mermaid or JSON. Verification findings are overlaid so gaps/conflicts/dead-rules show up coloured
// on the graph. It reuses the already-topologically-sorted cm.Decisions and verify.Report; it adds
// no new analysis.
package graph

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"

	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/verify"
)

// NodeKind distinguishes an input from a decision.
type NodeKind string

const (
	KindInput    NodeKind = "input"
	KindDecision NodeKind = "decision"
)

// Node is a graph vertex.
type Node struct {
	ID           string   `json:"id"`   // identifier-safe, stable, unique
	Name         string   `json:"name"` // original feelc name (qualified module__name in project mode)
	Kind         NodeKind `json:"kind"`
	Type         string   `json:"type,omitempty"`         // inputs: number/string/boolean
	HitPolicy    string   `json:"hitPolicy,omitempty"`    // decision tables
	DecisionKind string   `json:"decisionKind,omitempty"` // decisions: "table" | "expression"
	Module       string   `json:"module,omitempty"`       // project mode: the owning module (prefix before "__")
	Local        string   `json:"local,omitempty"`        // project mode: the unqualified name within its module
	Line         int      `json:"line,omitempty"`         // 1-based source line of a decision (0 if unknown)
	Severity     string   `json:"severity,omitempty"`     // worst finding severity (error>warning>info)
	Findings     []string `json:"findings,omitempty"`     // finding messages attached to this node
}

// Edge is a dependency: From feeds Into (From -> Into).
type Edge struct {
	From        string `json:"from"`
	Into        string `json:"into"`
	CrossModule bool   `json:"crossModule,omitempty"` // project mode: From and Into live in different modules
}

// Graph is the renderable DRG.
type Graph struct {
	Model string `json:"model"`
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

// Build assembles the DRG from a compiled model and (optionally) a verification report.
func Build(cm *ir.CompiledModel, rep *verify.Report) *Graph {
	g := &Graph{Model: cm.Name}
	idOf := map[string]string{}
	seen := map[string]bool{}
	mkID := func(name string) string {
		if id, ok := idOf[name]; ok {
			return id
		}
		base := sanitize(name)
		id := base
		for k := 1; seen[id]; k++ {
			id = base + "_" + strconv.Itoa(k)
		}
		seen[id] = true
		idOf[name] = id
		return id
	}

	// Findings grouped per decision (worst severity wins for the node colour).
	type agg struct {
		sev  string
		msgs []string
	}
	byDec := map[string]*agg{}
	if rep != nil {
		for _, f := range rep.Findings {
			a := byDec[f.Decision]
			if a == nil {
				a = &agg{}
				byDec[f.Decision] = a
			}
			a.msgs = append(a.msgs, string(f.Kind)+": "+f.Message)
			a.sev = worse(a.sev, string(f.Severity))
		}
	}

	// Input nodes (sorted for determinism).
	names := make([]string, 0, len(cm.Inputs))
	for n := range cm.Inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		mod, local := splitModule(n)
		g.Nodes = append(g.Nodes, Node{ID: mkID(n), Name: n, Kind: KindInput, Type: typeName(cm.Inputs[n]), Module: mod, Local: local})
	}

	// Decision nodes (declared/topological order).
	for i := range cm.Decisions {
		d := &cm.Decisions[i]
		mod, local := splitModule(d.Name)
		node := Node{ID: mkID(d.Name), Name: d.Name, Kind: KindDecision, Module: mod, Local: local, Line: d.Line, DecisionKind: "expression"}
		if d.Kind == ir.KindTable && d.Table != nil {
			node.HitPolicy = hitName(d.Table.HitPolicy)
			node.DecisionKind = "table"
		}
		if a := byDec[d.Name]; a != nil {
			node.Severity = a.sev
			node.Findings = a.msgs
		}
		g.Nodes = append(g.Nodes, node)
	}

	// Edges: each dependency feeds its decision.
	for i := range cm.Decisions {
		d := &cm.Decisions[i]
		into := idOf[d.Name]
		dMod, _ := splitModule(d.Name)
		for _, dep := range d.Deps {
			from, ok := idOf[dep]
			if !ok {
				// A dependency neither declared as input nor decision: surface it as an input node
				// rather than dropping the edge (honest: never hide a requirement).
				from = mkID(dep)
				mod, local := splitModule(dep)
				g.Nodes = append(g.Nodes, Node{ID: from, Name: dep, Kind: KindInput, Module: mod, Local: local})
			}
			depMod, _ := splitModule(dep)
			g.Edges = append(g.Edges, Edge{From: from, Into: into, CrossModule: dMod != "" && depMod != "" && dMod != depMod})
		}
	}
	return g
}

// splitModule splits a possibly-qualified name "module__local" into (module, local). A name without the
// "__" namespace separator (single-file mode, or an unqualified input) returns ("", name).
func splitModule(name string) (module, local string) {
	if i := strings.Index(name, "__"); i >= 0 {
		return name[:i], name[i+2:]
	}
	return "", name
}

// JSON renders the graph as indented JSON.
func (g *Graph) JSON() string {
	b, _ := json.MarshalIndent(g, "", "  ")
	return string(b)
}

// DOT renders the graph as Graphviz DOT.
func (g *Graph) DOT() string {
	var b strings.Builder
	b.WriteString("digraph DRG {\n  rankdir=LR;\n  node [fontname=\"sans-serif\"];\n")
	for _, n := range g.Nodes {
		// Escape the dynamic pieces; the "\n" separators are structural DOT newlines (not escaped).
		label := dotEscape(n.Name)
		switch {
		case n.Kind == KindInput && n.Type != "":
			label += "\\n(" + dotEscape(n.Type) + ")"
		case n.Kind == KindDecision && n.HitPolicy != "":
			label += "\\n[" + dotEscape(n.HitPolicy) + "]"
		}
		shape := "box"
		attrs := ""
		if n.Kind == KindInput {
			shape = "ellipse"
		}
		if c := dotColor(n.Severity); c != "" {
			attrs = ", style=filled, fillcolor=\"" + c + "\""
		}
		b.WriteString("  " + n.ID + " [label=\"" + label + "\", shape=" + shape + attrs + "];\n")
	}
	for _, e := range g.Edges {
		b.WriteString("  " + e.From + " -> " + e.Into + ";\n")
	}
	b.WriteString("}\n")
	return b.String()
}

// Mermaid renders the graph as a Mermaid flowchart (left-to-right). In project mode (>1 module) nodes are
// grouped into per-module subgraphs and cross-module dependencies are drawn as dashed edges, so the module
// boundaries and the wiring between them read at a glance.
func (g *Graph) Mermaid() string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")

	nodeDef := func(n Node, indent string) {
		label := n.Name
		if n.Module != "" {
			label = n.Local // inside a module subgraph the prefix is redundant
		}
		switch {
		case n.Kind == KindInput && n.Type != "":
			label += "<br/>" + n.Type
		case n.Kind == KindDecision && n.HitPolicy != "":
			label += "<br/>" + n.HitPolicy
		}
		label = "\"" + strings.ReplaceAll(label, "\"", "'") + "\""
		if n.Kind == KindInput {
			b.WriteString(indent + n.ID + "([" + label + "])\n") // stadium = input
		} else {
			b.WriteString(indent + n.ID + "[" + label + "]\n") // rectangle = decision
		}
	}

	if mods := orderedModules(g.Nodes); len(mods) > 1 {
		for _, mod := range mods {
			if mod == "" {
				continue // ungrouped nodes emitted after the subgraphs
			}
			b.WriteString("  subgraph " + sanitize(mod) + "[\"" + strings.ReplaceAll(mod, "\"", "'") + "\"]\n")
			for _, n := range g.Nodes {
				if n.Module == mod {
					nodeDef(n, "    ")
				}
			}
			b.WriteString("  end\n")
		}
		for _, n := range g.Nodes {
			if n.Module == "" {
				nodeDef(n, "  ")
			}
		}
	} else {
		for _, n := range g.Nodes {
			nodeDef(n, "  ")
		}
	}

	for _, e := range g.Edges {
		arrow := " --> "
		if e.CrossModule {
			arrow = " -.-> " // cross-module dependency: dashed
		}
		b.WriteString("  " + e.From + arrow + e.Into + "\n")
	}
	// Severity classes + assignments.
	b.WriteString("  classDef error fill:#e06b6b,stroke:#a33,color:#1a1a1a;\n")
	b.WriteString("  classDef warning fill:#d9a441,stroke:#a06f1a,color:#1a1a1a;\n")
	b.WriteString("  classDef info fill:#7fb3ff,stroke:#3a6fb0,color:#1a1a1a;\n")
	for _, n := range g.Nodes {
		if n.Severity != "" {
			b.WriteString("  class " + n.ID + " " + n.Severity + ";\n")
		}
	}
	return b.String()
}

// orderedModules returns the distinct module names in first-appearance order (empty string = ungrouped).
func orderedModules(nodes []Node) []string {
	var out []string
	seen := map[string]bool{}
	for _, n := range nodes {
		if !seen[n.Module] {
			seen[n.Module] = true
			out = append(out, n.Module)
		}
	}
	return out
}

// dotEscape escapes a value for a DOT double-quoted string (backslash and quote).
func dotEscape(s string) string {
	return strings.NewReplacer("\\", "\\\\", "\"", "\\\"").Replace(s)
}

func sanitize(name string) string {
	var b strings.Builder
	b.WriteString("n_")
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func typeName(t ir.Type) string {
	switch t {
	case ir.TypeNumber:
		return "number"
	case ir.TypeString:
		return "string"
	case ir.TypeBool:
		return "boolean"
	case ir.TypeContext:
		return "context"
	}
	return ""
}

func hitName(h ir.HitPolicy) string {
	switch h {
	case ir.HitUnique:
		return "unique"
	case ir.HitAny:
		return "any"
	case ir.HitFirst:
		return "first"
	case ir.HitPriority:
		return "priority"
	case ir.HitCollect:
		return "collect"
	case ir.HitRuleOrder:
		return "rule order"
	case ir.HitOutputOrder:
		return "output order"
	}
	return ""
}

// worse returns the higher-priority severity of a and b (error > warning > info > "").
func worse(a, b string) string {
	rank := map[string]int{"": 0, "info": 1, "warning": 2, "error": 3}
	if rank[b] > rank[a] {
		return b
	}
	return a
}

func dotColor(sev string) string {
	switch sev {
	case "error":
		return "#e06b6b"
	case "warning":
		return "#d9a441"
	case "info":
		return "#cfe0ff"
	}
	return ""
}
