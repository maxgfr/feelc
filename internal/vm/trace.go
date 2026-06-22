package vm

import (
	"fmt"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/ir"
)

// DecisionTrace : justification d'une décision (règle gagnante + cellules justifiantes + sortie).
// JSON-able. La sémantique de matching reste centralisée dans ir.MatchCell / la VM : Trace REJOUE
// l'évaluation, il ne la duplique pas (pas de divergence possible avec engine.Eval).
type DecisionTrace struct {
	Decision     string      `json:"decision"`
	Kind         string      `json:"kind"` // "table" | "literal-expr"
	HitPolicy    string      `json:"hitPolicy,omitempty"`
	Matched      bool        `json:"matched"`
	Fallback     bool        `json:"fallback,omitempty"`     // sortie via `default` (ou null)
	RuleIndex    int         `json:"ruleIndex,omitempty"`    // 1-based, règle gagnante (single-hit)
	RuleLine     int         `json:"ruleLine,omitempty"`     // ligne source de la règle gagnante
	Cells        []CellTrace `json:"cells,omitempty"`        // cellules justifiantes (test vrai, non `-`)
	Contributors []RuleRef   `json:"contributors,omitempty"` // COLLECT / RULE ORDER : règles contributrices
	Output       any         `json:"output"`
	ExprSrc      string      `json:"exprSrc,omitempty"`      // literal-expr : source de l'expression
	NotGeometric bool        `json:"notGeometric,omitempty"` // justification évaluée (Op=Prog / expression), non géométrique
}

// CellTrace : une cellule qui justifie le match de la règle gagnante.
type CellTrace struct {
	Input string `json:"input"`
	Src   string `json:"src"`
	Line  int    `json:"line,omitempty"`
	Value string `json:"value"` // valeur de la colonne au moment de l'évaluation
}

// RuleRef : référence à une règle (COLLECT / RULE ORDER).
type RuleRef struct {
	Index int `json:"index"`
	Line  int `json:"line,omitempty"`
}

// Trace évalue une décision en CAPTURANT sa justification.
func Trace(cm *ir.CompiledModel, decisionName string, inputs map[string]ir.Value) (*DecisionTrace, error) {
	dec, ok := cm.Decision(decisionName)
	if !ok {
		return nil, fmt.Errorf("décision inconnue: %q", decisionName)
	}
	e := &evaluator{cm: cm, inputs: inputs, memo: map[string]ir.Value{}, state: map[string]int{}}
	tr := &DecisionTrace{Decision: decisionName}
	switch dec.Kind {
	case ir.KindLiteralExpr:
		out, err := e.evalExpr(dec.Expr, nil)
		if err != nil {
			return nil, err
		}
		tr.Kind = "literal-expr"
		tr.Matched = true
		tr.ExprSrc = dec.ExprSrc
		tr.NotGeometric = true // une expression n'est pas une justification géométrique (honnêteté)
		tr.Output = out.ToAny()
		return tr, nil
	case ir.KindTable:
		if err := e.traceTable(dec.Table, tr); err != nil {
			return nil, err
		}
		return tr, nil
	default:
		return nil, fmt.Errorf("décision %q: type non traçable", decisionName)
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

	// COLLECT / RULE ORDER : la justification est l'ensemble des règles contributrices.
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

	// Single-hit : déterminer la VRAIE règle retenue par la politique (pas juste le 1er match).
	winner := -1
	switch t.HitPolicy {
	case ir.HitFirst:
		if len(matched) > 0 {
			winner = matched[0]
		}
	case ir.HitUnique:
		if len(matched) > 1 {
			return fmt.Errorf("hit policy UNIQUE: %d règles matchent (au plus 1 attendue)", len(matched))
		}
		if len(matched) == 1 {
			winner = matched[0]
		}
	case ir.HitAny:
		if len(matched) > 0 {
			for _, ri := range matched[1:] {
				if !outputsEqual(t.Rules[ri].Outputs, t.Rules[matched[0]].Outputs) {
					return fmt.Errorf("hit policy ANY: règles en conflit (sorties divergentes)")
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
		return fmt.Errorf("hit policy non traçable")
	}

	if winner < 0 {
		out, err := e.fallback(t)
		if err != nil {
			return err
		}
		tr.Fallback = true
		tr.Output = out.ToAny()
		return nil
	}

	tr.Matched = true
	tr.RuleIndex = winner + 1
	tr.RuleLine = t.Rules[winner].Line
	tr.Output = buildOutput(t.Outputs, t.Rules[winner].Outputs).ToAny()
	for i, ct := range t.Rules[winner].Conds {
		if ct.Op == ir.OpAny {
			continue // `-` : ne justifie rien
		}
		tr.Cells = append(tr.Cells, CellTrace{Input: t.Inputs[i], Src: ct.Src, Line: ct.Line, Value: traceValue(cols[i])})
		if ct.Op == ir.OpProg {
			tr.NotGeometric = true // cellule expression : justification évaluée, non géométrique
		}
	}
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
