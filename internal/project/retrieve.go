package project

import (
	"fmt"
	"sort"
	"strings"

	"github.com/maxgfr/feelc/internal/ir"
)

// retrieve.go builds a bounded, LEXICALLY-ranked context block to hand an LLM when authoring/editing a
// module inside a large project — WITHOUT embeddings (stdlib only, deterministic, "no LLM in the core").
// The block holds the target module's source, the cross-module decisions it may bind to, and the top-K
// other modules ranked by token overlap with the request, so the model stays within its context window
// even when the project has hundreds of rules.

const (
	defaultRetrieveK = 6
	maxContextBytes  = 12000 // hard cap so a huge project can't blow the LLM context window
	minTermLen       = 3
)

// RetrieveContext returns the context block for editing targetModule given a natural-language query.
func (p *Project) RetrieveContext(query, targetModule string, k int) string {
	if k <= 0 {
		k = defaultRetrieveK
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Project %q (%d modules)\n", p.Manifest.Name, len(p.Modules))

	if target, ok := p.Module(targetModule); ok {
		fmt.Fprintf(&b, "\n## Target module to edit: %s\n```rules\n%s\n```\n",
			target.Name, strings.TrimRight(string(target.Source), "\n"))
		if len(target.Uses) > 0 {
			b.WriteString("\n## Cross-module decisions this module may reference (declare each as an `input`, wired by the manifest `uses`):\n")
			for _, alias := range sortedKeys(target.Uses) {
				fmt.Fprintf(&b, "- input %s  ⟵  %s\n", alias, target.Uses[alias])
			}
		}
	}

	// Rank the OTHER modules by lexical overlap with the query, ties broken by name (deterministic).
	terms := tokenize(query)
	type scored struct {
		m     *Module
		score int
	}
	ranked := make([]scored, 0, len(p.Modules))
	for _, m := range p.Modules {
		if m.Name == targetModule {
			continue
		}
		ranked = append(ranked, scored{m, scoreModule(m, terms)})
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].m.Name < ranked[j].m.Name
	})
	if len(ranked) > k {
		ranked = ranked[:k]
	}
	if len(ranked) > 0 {
		b.WriteString("\n## Other modules (signatures — reference only, do not edit):\n")
		for _, sc := range ranked {
			if b.Len() >= maxContextBytes {
				b.WriteString("… (truncated)\n")
				break
			}
			b.WriteString(moduleSignature(sc.m))
		}
	}
	out := b.String()
	if len(out) > maxContextBytes {
		out = out[:maxContextBytes] + "\n… (truncated)\n"
	}
	return out
}

// moduleSignature renders a compact, name-qualified signature of a module (inputs + decisions + titles).
func moduleSignature(m *Module) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n### module %s\n", m.Name)
	if ins := sortedKeys(m.Model.Inputs); len(ins) > 0 {
		parts := make([]string, len(ins))
		for i, n := range ins {
			parts[i] = n + " : " + typeName(m.Model.Inputs[n])
		}
		fmt.Fprintf(&b, "inputs: %s\n", strings.Join(parts, ", "))
	}
	for i := range m.Model.Decisions {
		d := &m.Model.Decisions[i]
		line := fmt.Sprintf("decision %s.%s", m.Name, d.Name)
		if d.Meta.Title != "" {
			line += "  — " + d.Meta.Title
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// scoreModule counts how many query terms appear in the module's searchable text (name, inputs,
// decision names, metadata titles/docs/source). A simple, transparent lexical relevance signal.
func scoreModule(m *Module, terms map[string]bool) int {
	if len(terms) == 0 {
		return 0
	}
	hay := moduleHaystack(m)
	score := 0
	for t := range terms {
		if strings.Contains(hay, t) {
			score++
		}
	}
	return score
}

// moduleHaystack is the lowercased searchable text of a module.
func moduleHaystack(m *Module) string {
	var b strings.Builder
	b.WriteString(m.Name)
	b.WriteByte(' ')
	for _, n := range sortedKeys(m.Model.Inputs) {
		b.WriteString(n)
		b.WriteByte(' ')
		if meta, ok := m.Model.InputMeta[n]; ok {
			b.WriteString(meta.Title + " " + meta.Doc + " ")
		}
	}
	for i := range m.Model.Decisions {
		d := &m.Model.Decisions[i]
		b.WriteString(d.Name + " " + d.Meta.Title + " " + d.Meta.Doc + " ")
	}
	return strings.ToLower(b.String())
}

// tokenize lowercases the query and splits it into a set of meaningful terms.
func tokenize(s string) map[string]bool {
	out := map[string]bool{}
	for _, f := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9')
	}) {
		if len(f) >= minTermLen {
			out[f] = true
		}
	}
	return out
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
	case ir.TypeDate:
		return "date"
	case ir.TypeDuration:
		return "duration"
	}
	return "unknown"
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
