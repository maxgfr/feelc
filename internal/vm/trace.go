package vm

import (
	"encoding/json"
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/ir"
)

// DecisionTrace: justification of a decision (winning rule + justifying cells + output).
// JSON-able. The matching semantics stay centralized in ir.MatchCell / the VM: Trace REPLAYS
// the evaluation, it does not duplicate it (no possible divergence with engine.Eval).
type DecisionTrace struct {
	Decision     string      `json:"decision"`
	Title        string      `json:"title,omitempty"`  // @title annotation, if any
	Source       string      `json:"source,omitempty"` // @source traceability (e.g. law article), if any
	Kind         string      `json:"kind"`             // "table" | "literal-expr"
	HitPolicy    string      `json:"hitPolicy,omitempty"`
	Matched      bool        `json:"matched"`
	Fallback     bool        `json:"fallback,omitempty"`     // output via `default` (or null)
	RuleIndex    int         `json:"ruleIndex,omitempty"`    // 1-based, winning rule (single-hit)
	RuleLine     int         `json:"ruleLine,omitempty"`     // source line of the winning rule
	Cells        []CellTrace `json:"cells,omitempty"`        // justifying cells (test true, not `-`)
	Contributors []RuleRef   `json:"contributors,omitempty"` // COLLECT / RULE ORDER: contributing rules
	Output       any         `json:"output"`
	ExprSrc      string      `json:"exprSrc,omitempty"`      // literal-expr: source of the expression
	NotGeometric bool        `json:"notGeometric,omitempty"` // evaluated justification (Op=Prog / expression), not geometric
}

// CellTrace: a cell that justifies the match of the winning rule.
type CellTrace struct {
	Input string `json:"input"`
	Src   string `json:"src"`
	Line  int    `json:"line,omitempty"`
	Value string `json:"value"` // column value at evaluation time
}

// RuleRef: reference to a rule (COLLECT / RULE ORDER).
type RuleRef struct {
	Index int `json:"index"`
	Line  int `json:"line,omitempty"`
}

// Trace evaluates a decision while CAPTURING its justification.
func Trace(cm *ir.CompiledModel, decisionName string, inputs map[string]ir.Value) (*DecisionTrace, error) {
	e := &evaluator{cm: cm, inputs: inputs, memo: map[string]ir.Value{}, state: map[string]int{}}
	return e.trace(decisionName)
}

// trace replays ONE decision on the shared evaluator e, capturing its justification. Keeping it a
// method (vs. a free function) lets TraceFull drive a single evaluator across a whole DRG path so
// memoization matches engine.Eval exactly (no divergence).
func (e *evaluator) trace(decisionName string) (*DecisionTrace, error) {
	dec, ok := e.cm.Decision(decisionName)
	if !ok {
		return nil, fmt.Errorf("unknown decision: %q", decisionName)
	}
	tr := &DecisionTrace{Decision: decisionName, Title: dec.Meta.Title, Source: dec.Meta.Source}
	switch dec.Kind {
	case ir.KindLiteralExpr:
		out, err := e.evalExpr(dec.Expr, nil)
		if err != nil {
			return nil, err
		}
		tr.Kind = "literal-expr"
		tr.Matched = true
		tr.ExprSrc = dec.ExprSrc
		tr.NotGeometric = true // an expression is not a geometric justification (honesty)
		tr.Output = out.ToAny()
		return tr, nil
	case ir.KindTable:
		if err := e.traceTable(dec.Table, tr); err != nil {
			return nil, err
		}
		return tr, nil
	default:
		return nil, fmt.Errorf("decision %q: untraceable type", decisionName)
	}
}

// SourceCitation links a decision on the trace path to its @source annotation (traceability seed).
type SourceCitation struct {
	Decision string `json:"decision"`
	Source   string `json:"source"`
	Title    string `json:"title,omitempty"`
}

// FullTrace is the justification of a goal decision AND every upstream decision it transitively
// consumed: the path through the DRG in dependency-first order (goal last), each entry a
// self-contained DecisionTrace. Deterministic — it REPLAYS Eval on a single shared evaluator, so
// memoization matches the engine exactly and the same input always yields the same trace.
type FullTrace struct {
	Goal    string           `json:"goal"`
	Inputs  map[string]any   `json:"inputs"`
	Path    []*DecisionTrace `json:"path"`              // upstream decisions then the goal
	Result  *DecisionTrace   `json:"result"`            // == Path[len-1]; convenience alias
	Sources []SourceCitation `json:"sources,omitempty"` // distinct @source citations on the path
}

// TraceFull traces a goal decision AND the whole upstream DRG path it depends on. It walks the
// decisions returned by RequiredDecisions(goal) (dependency-first) on one shared evaluator.
func TraceFull(cm *ir.CompiledModel, goal string, inputs map[string]ir.Value) (*FullTrace, error) {
	order, err := cm.RequiredDecisions(goal)
	if err != nil {
		return nil, err
	}
	e := &evaluator{cm: cm, inputs: inputs, memo: map[string]ir.Value{}, state: map[string]int{}}
	ft := &FullTrace{Goal: goal, Inputs: jsonInputs(inputs)}
	for _, name := range order {
		tr, err := e.trace(name)
		if err != nil {
			return nil, err
		}
		ft.Path = append(ft.Path, tr)
		if tr.Source != "" {
			ft.Sources = append(ft.Sources, SourceCitation{Decision: tr.Decision, Source: tr.Source, Title: tr.Title})
		}
	}
	if len(ft.Path) > 0 {
		ft.Result = ft.Path[len(ft.Path)-1]
	}
	return ft, nil
}

func jsonInputs(inputs map[string]ir.Value) map[string]any {
	out := make(map[string]any, len(inputs))
	for k, v := range inputs {
		out[k] = cleanNumbers(v.ToAny())
	}
	return out
}

// cleanNumbers renders decimals as clean json.Number (e.g. "10", not "1E+1"), matching how the rest
// of the API presents numbers — so the echoed inputs read naturally in the trace/audit.
func cleanNumbers(v any) any {
	switch x := v.(type) {
	case *apd.Decimal:
		r := new(apd.Decimal)
		r.Reduce(x)
		return json.Number(r.Text('f'))
	case []any:
		for i := range x {
			x[i] = cleanNumbers(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = cleanNumbers(x[k])
		}
		return x
	default:
		return v
	}
}

func (e *evaluator) traceTable(t *ir.DecisionTable, tr *DecisionTrace) error {
	tr.Kind = "table"
	tr.HitPolicy = hitPolicyName(t.HitPolicy)

	cols := make([]ir.Value, len(t.Inputs))
	for i, name := range t.Inputs {
		v, err := e.resolve(name)
		if err != nil {
			return err
		}
		cols[i] = v
	}

	// FIRST: short-circuits at the 1st matching rule, EXACTLY like evalTable. Do NOT evaluate
	// the following rules: a later Op=Prog cell that errors (e.g. division by zero) would make
	// Trace fail where Eval succeeds → divergence. (Adversarial review, Slice 4.)
	if t.HitPolicy == ir.HitFirst {
		for ri := range t.Rules {
			ok, err := e.matches(t.Rules[ri], cols)
			if err != nil {
				return err
			}
			if ok {
				e.fillWinner(t, ri, cols, tr)
				return nil
			}
		}
		return e.fillFallback(t, tr)
	}

	var matched []int
	for ri := range t.Rules {
		ok, err := e.matches(t.Rules[ri], cols)
		if err != nil {
			return err
		}
		if ok {
			matched = append(matched, ri)
		}
	}

	// COLLECT / RULE ORDER: the justification is the set of contributing rules.
	if t.HitPolicy == ir.HitCollect || t.HitPolicy == ir.HitRuleOrder {
		rules := make([]ir.Rule, len(matched))
		for i, ri := range matched {
			rules[i] = t.Rules[ri]
			tr.Contributors = append(tr.Contributors, RuleRef{Index: ri + 1, Line: t.Rules[ri].Line})
		}
		out, err := e.collect(t, rules)
		if err != nil {
			return err
		}
		tr.Matched = len(matched) > 0
		tr.Output = out.ToAny()
		return nil
	}

	// UNIQUE / ANY / PRIORITY: these policies evaluate ALL rules (like evalTable), so
	// no divergence is possible. We determine the REAL rule selected.
	winner := -1
	switch t.HitPolicy {
	case ir.HitUnique:
		if len(matched) > 1 {
			return fmt.Errorf("hit policy UNIQUE: %d rules match (at most 1 expected)", len(matched))
		}
		if len(matched) == 1 {
			winner = matched[0]
		}
	case ir.HitAny:
		if len(matched) > 0 {
			for _, ri := range matched[1:] {
				if !outputsEqual(t.Rules[ri].Outputs, t.Rules[matched[0]].Outputs) {
					return fmt.Errorf("hit policy ANY: conflicting rules (divergent outputs)")
				}
			}
			winner = matched[0]
		}
	case ir.HitPriority:
		if len(matched) > 0 {
			winner = matched[0]
			bestRank := rank(t.Priority, t.Rules[matched[0]].Outputs[0])
			for _, ri := range matched[1:] {
				if rk := rank(t.Priority, t.Rules[ri].Outputs[0]); rk < bestRank {
					winner, bestRank = ri, rk
				}
			}
		}
	default:
		return fmt.Errorf("untraceable hit policy")
	}

	if winner < 0 {
		return e.fillFallback(t, tr)
	}
	e.fillWinner(t, winner, cols, tr)
	return nil
}

// fillWinner fills in the winning rule + its justifying cells (test true, not `-`).
func (e *evaluator) fillWinner(t *ir.DecisionTable, winner int, cols []ir.Value, tr *DecisionTrace) {
	tr.Matched = true
	tr.RuleIndex = winner + 1
	tr.RuleLine = t.Rules[winner].Line
	tr.Output = buildOutput(t.Outputs, t.Rules[winner].Outputs).ToAny()
	for i, ct := range t.Rules[winner].Conds {
		if ct.Op == ir.OpAny {
			continue // `-`: justifies nothing
		}
		tr.Cells = append(tr.Cells, CellTrace{Input: t.Inputs[i], Src: ct.Src, Line: ct.Line, Value: traceValue(cols[i])})
		if ct.Op == ir.OpProg {
			tr.NotGeometric = true // expression cell: evaluated justification, not geometric
		}
	}
}

func (e *evaluator) fillFallback(t *ir.DecisionTable, tr *DecisionTrace) error {
	out, err := e.fallback(t)
	if err != nil {
		return err
	}
	tr.Fallback = true
	tr.Output = out.ToAny()
	return nil
}

func hitPolicyName(h ir.HitPolicy) string {
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
	}
	return "?"
}

func traceValue(v ir.Value) string {
	switch v.Tag {
	case ir.TagNumber:
		r := new(apd.Decimal)
		r.Reduce(v.Num)
		return r.Text('f')
	case ir.TagString:
		return v.Str
	case ir.TagBool:
		if v.Bool {
			return "true"
		}
		return "false"
	default:
		return "null"
	}
}
