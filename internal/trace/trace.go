// Package trace produces a SOURCE TRACEABILITY report for a compiled model: it links each
// decision (and input) to its @source annotation, flags decisions that cite no source, and —
// when the raw specification text is available — offers best-effort coverage of which source
// paragraphs are referenced by some rule. It is LLM-free and deterministic.
//
// HONESTY: @source is a free-text citation (e.g. "HR policy 4.2"), not a structured span. The
// decision↔citation map is therefore reliable, but source-span coverage is a HEURISTIC and is
// reported as advisory — never as a proof that a span is or isn't covered.
package trace

import (
	"sort"
	"strings"

	"github.com/maxgfr/feelc/internal/ir"
)

// Report is the traceability of a model: decisions/inputs with their @source, the untraced
// decisions (rule-side gaps), optional source-span coverage, and headline numbers.
type Report struct {
	Decisions []DecisionTrace `json:"decisions"`
	Inputs    []InputTrace    `json:"inputs"`
	Untraced  []string        `json:"untraced"` // decisions citing no @source
	Spans     []SpanCoverage  `json:"spans,omitempty"`
	Coverage  Coverage        `json:"coverage"`
}

// DecisionTrace is a decision and the @source it cites (if any).
type DecisionTrace struct {
	Decision string   `json:"decision"`
	Title    string   `json:"title,omitempty"`
	Source   string   `json:"source,omitempty"`
	Kind     string   `json:"kind"` // "table" | "expression"
	Deps     []string `json:"deps,omitempty"`
}

// InputTrace is an input and the @source it cites (if any).
type InputTrace struct {
	Name   string `json:"name"`
	Source string `json:"source,omitempty"`
}

// SpanCoverage is one source paragraph and whether any rule's @source heuristically references it.
type SpanCoverage struct {
	Span    string   `json:"span"` // the paragraph text (truncated for display)
	Line    int      `json:"line"` // 1-based start line in the source
	Covered bool     `json:"covered"`
	By      []string `json:"by,omitempty"` // names whose @source matched this span
}

// Coverage holds the headline numbers.
type Coverage struct {
	Decisions        int `json:"decisions"`
	DecisionsSourced int `json:"decisionsSourced"`
	SpansTotal       int `json:"spansTotal,omitempty"`
	SpansCovered     int `json:"spansCovered,omitempty"`
}

// Build assembles the rule-side report from the compiled model alone (no source text needed).
func Build(cm *ir.CompiledModel) *Report {
	r := &Report{}
	for i := range cm.Decisions {
		d := &cm.Decisions[i]
		r.Decisions = append(r.Decisions, DecisionTrace{
			Decision: d.Name, Title: d.Meta.Title, Source: d.Meta.Source, Kind: kindName(d.Kind), Deps: d.Deps,
		})
		if d.Meta.Source == "" {
			r.Untraced = append(r.Untraced, d.Name)
		} else {
			r.Coverage.DecisionsSourced++
		}
	}
	r.Coverage.Decisions = len(cm.Decisions)

	names := make([]string, 0, len(cm.Inputs))
	for n := range cm.Inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		r.Inputs = append(r.Inputs, InputTrace{Name: n, Source: cm.InputMeta[n].Source})
	}
	return r
}

// BuildWithSource adds best-effort source-span coverage on top of Build: the raw spec text is
// split into paragraphs and each is matched HEURISTICALLY against decision/input @source
// citations. Advisory only.
func BuildWithSource(cm *ir.CompiledModel, source []byte) *Report {
	r := Build(cm)
	type cite struct{ name, text string }
	var cites []cite
	for i := range cm.Decisions {
		if s := cm.Decisions[i].Meta.Source; s != "" {
			cites = append(cites, cite{cm.Decisions[i].Name, s})
		}
	}
	for _, in := range r.Inputs {
		if in.Source != "" {
			cites = append(cites, cite{in.Name, in.Source})
		}
	}
	for _, sp := range splitSpans(string(source)) {
		cov := SpanCoverage{Span: truncate(sp.text, 200), Line: sp.line}
		for _, c := range cites {
			if matches(c.text, sp.text) {
				cov.Covered = true
				cov.By = append(cov.By, c.name)
			}
		}
		sort.Strings(cov.By)
		if cov.Covered {
			r.Coverage.SpansCovered++
		}
		r.Spans = append(r.Spans, cov)
	}
	r.Coverage.SpansTotal = len(r.Spans)
	return r
}

func kindName(k ir.DecisionKind) string {
	if k == ir.KindTable {
		return "table"
	}
	return "expression"
}

type span struct {
	text string
	line int // 1-based start line
}

// splitSpans groups the source into paragraphs separated by blank lines.
func splitSpans(src string) []span {
	lines := strings.Split(src, "\n")
	var spans []span
	var cur []string
	start := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		if text := strings.TrimSpace(strings.Join(cur, " ")); text != "" {
			spans = append(spans, span{text: text, line: start + 1})
		}
		cur = nil
	}
	for i, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			flush()
			continue
		}
		if len(cur) == 0 {
			start = i
		}
		cur = append(cur, strings.TrimSpace(ln))
	}
	flush()
	return spans
}

// matches is the heuristic: a citation matches a span when at least 60% of the citation's
// distinctive tokens appear in the span. Conservative by design (avoids false "covered").
func matches(citation, spanText string) bool {
	ct := distinctiveTokens(citation)
	if len(ct) == 0 {
		return false
	}
	st := distinctiveTokens(spanText)
	hit := 0
	for tok := range ct {
		if st[tok] {
			hit++
		}
	}
	return float64(hit)/float64(len(ct)) >= 0.6
}

// distinctiveTokens returns the set of "meaningful" tokens of s: words of length >= 4 or any
// token containing a digit (so identifiers like "4.2" or "L1225" count). Short common words
// ("the", "and", "for") are dropped to avoid diluting the match.
func distinctiveTokens(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if isDistinctive(t) {
			out[t] = true
		}
	}
	return out
}

func isDistinctive(t string) bool {
	if len(t) >= 4 {
		return true
	}
	for _, r := range t {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
