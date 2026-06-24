// Package modelinfo extracts UI-facing metadata from a compiled model — the inputs (typed, with
// readable domains) and decisions (kind + hit policy) — plus JSONify, the decimal->json.Number
// normalizer. It is the single source of truth shared by the HTTP service (internal/service) and
// the WebAssembly playground entrypoint (cmd/feelc-wasm), so the two surfaces never drift.
package modelinfo

import (
	"encoding/json"
	"sort"
	"strings"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/ir"
)

// InputInfo describes a model input for typed widgets, the simulator and docs.
type InputInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Domain   string `json:"domain,omitempty"`
	Unit     string `json:"unit,omitempty"`
	Title    string `json:"title,omitempty"`
	Question string `json:"question,omitempty"`
	Doc      string `json:"doc,omitempty"`
	Source   string `json:"source,omitempty"`
}

// DecInfo describes a decision: its kind and (for tables) hit policy, plus metadata.
type DecInfo struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	HitPolicy string   `json:"hitPolicy,omitempty"`
	Deps      []string `json:"deps,omitempty"`
	Unit      string   `json:"unit,omitempty"`
	Title     string   `json:"title,omitempty"`
	Doc       string   `json:"doc,omitempty"`
	Source    string   `json:"source,omitempty"`
}

// Inputs returns the model inputs (typed, with readable domains), sorted by name.
func Inputs(cm *ir.CompiledModel) []InputInfo {
	names := make([]string, 0, len(cm.Inputs))
	for n := range cm.Inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]InputInfo, 0, len(names))
	for _, n := range names {
		m := cm.InputMeta[n]
		out = append(out, InputInfo{
			Name: n, Type: TypeName(cm.Inputs[n]), Domain: DomainString(cm.Domains[n]), Unit: cm.Units[n],
			Title: m.Title, Question: m.Question, Doc: m.Doc, Source: m.Source,
		})
	}
	return out
}

// Decisions returns the decisions with their kind ("table" | "literal-expr") and hit policy.
func Decisions(cm *ir.CompiledModel) []DecInfo {
	out := make([]DecInfo, len(cm.Decisions))
	for i := range cm.Decisions {
		d := &cm.Decisions[i]
		info := DecInfo{Name: d.Name, Deps: d.Deps, Unit: cm.Units[d.Name], Title: d.Meta.Title, Doc: d.Meta.Doc, Source: d.Meta.Source}
		if d.Kind == ir.KindTable && d.Table != nil {
			info.Kind = "table"
			info.HitPolicy = HitPolicyName(d.Table.HitPolicy)
		} else {
			info.Kind = "literal-expr"
		}
		out[i] = info
	}
	return out
}

// TypeName renders a scalar type as its DSL keyword.
func TypeName(t ir.Type) string {
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
	return ""
}

// DomainString renders a Domain back to its readable DSL form (for UI widgets + docs).
func DomainString(d ir.Domain) string {
	switch d.Kind {
	case ir.DomNumeric:
		if d.LoInf && d.HiInf {
			return ""
		}
		lo, hi := "-inf", "+inf"
		if !d.LoInf {
			lo = numText(d.Lo)
		}
		if !d.HiInf {
			hi = numText(d.Hi)
		}
		lb, rb := "[", "]"
		if d.LoOpen {
			lb = "("
		}
		if d.HiOpen {
			rb = ")"
		}
		return "in " + lb + lo + ".." + hi + rb
	case ir.DomEnum:
		parts := make([]string, len(d.Enum))
		for i, v := range d.Enum {
			parts[i] = valText(v)
		}
		return "in {" + strings.Join(parts, ", ") + "}"
	}
	return ""
}

// HitPolicyName renders a hit policy as its DSL keyword.
func HitPolicyName(h ir.HitPolicy) string {
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

// JSONify converts decimals (recursively in lists/contexts) to json.Number for clean numeric
// serialization (so 10 renders as "10", not "1E+1").
func JSONify(v any) any {
	switch x := v.(type) {
	case *apd.Decimal:
		return json.Number(x.Text('f'))
	case []any:
		for i := range x {
			x[i] = JSONify(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = JSONify(x[k])
		}
		return x
	default:
		return v
	}
}

func numText(v ir.Value) string {
	if v.Tag == ir.TagNumber && v.Num != nil {
		r := new(apd.Decimal)
		r.Reduce(v.Num)
		return r.Text('f')
	}
	return ""
}

func valText(v ir.Value) string {
	switch v.Tag {
	case ir.TagNumber:
		return numText(v)
	case ir.TagString:
		return "\"" + v.Str + "\""
	case ir.TagBool:
		if v.Bool {
			return "true"
		}
		return "false"
	}
	return ""
}
