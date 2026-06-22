// Package verify is feelc's differentiator: it PROVES properties of a decision
// table by geometric decomposition into atomic cells.
//
// Principle: on each dimension (input column) we collect the cut points (cell
// boundaries + domain boundaries). The cartesian product of representative witness
// points exhaustively covers the space; for each point we count the rules that match
// (via the SAME ir.MatchCell function as the VM -> proof and execution agree). From this we derive:
//   - completeness: a point covered by 0 rules = GAP (with a concrete counterexample);
//   - conflicts   : depending on the hit policy (UNIQUE -> any overlap; ANY -> divergent outputs);
//   - dead / masked rules (FIRST/PRIORITY); useless `default` line;
//   - subsumption (ANY/PRIORITY): a rule whose region is included in another with the same
//     output -> REDUNDANT (removable), via an inclusion matrix (bitset) on the same grid.
//
// Honest degradation: a table with an Op=Prog (non-geometric) cell or a grid that is too
// large is reported "not formally proven" — never silently conformed.
package verify

import (
	"fmt"
	"sort"
	"strings"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// gridBudget bounds the size of the atomic grid (anti-explosion guard).
const gridBudget = 500_000

// maxWitnessesPerKind limits the number of counterexamples reported per category.
const maxWitnessesPerKind = 5

type Kind string

const (
	KindGap                Kind = "gap"
	KindConflict           Kind = "conflict"
	KindDeadRule           Kind = "dead-rule"
	KindUnreachableDefault Kind = "unreachable-default"
	KindNotVerifiable      Kind = "not-verifiable"
	KindSubsumed           Kind = "subsumed" // rule whose region is included in another
)

// maxSubsumeRules: the subsumption matrix fits in a uint64 bitset up to 64 rules
// (the 8×50 bench table fits). Beyond that, subsumption analysis is honestly omitted.
const maxSubsumeRules = 64

type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// Finding: a verification diagnostic.
type Finding struct {
	Decision string            `json:"decision"`
	Kind     Kind              `json:"kind"`
	Severity Severity          `json:"severity"`
	Message  string            `json:"message"`
	Witness  map[string]string `json:"witness,omitempty"` // witness point (input -> value)
	Rules    []int             `json:"rules,omitempty"`   // rules concerned (1-based)
}

// Report: the set of diagnostics.
type Report struct {
	Findings []Finding `json:"findings"`
}

func (r *Report) add(f Finding) { r.Findings = append(r.Findings, f) }

// Blockers counts the blocking findings (error severity).
func (r *Report) Blockers() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

// Verify analyzes all decision tables of a compiled model.
func Verify(cm *ir.CompiledModel) *Report {
	rep := &Report{}
	for i := range cm.Decisions {
		d := &cm.Decisions[i]
		if d.Kind == ir.KindTable {
			verifyTable(cm, d, rep)
		}
	}
	return rep
}

type dim struct {
	col       string
	witnesses []ir.Value
}

// smtProve is the EXTENSION POINT for an SMT backend (z3), enabled by the build tag `smt`
// (cf. verify_smt.go, ADR 0007). nil by default: tables with Op=Prog cells (non-geometric)
// stay in honest degradation `not-verifiable`. A backend must return true if it HANDLED the
// decision (added its findings), false to leave the default not-verifiable.
var smtProve func(cm *ir.CompiledModel, d *ir.Decision, rep *Report) bool

func verifyTable(cm *ir.CompiledModel, d *ir.Decision, rep *Report) {
	t := d.Table

	// Op=Prog (non-geometric) cell: route to the SMT backend if it is plugged in, otherwise
	// honest degradation `not-verifiable`.
	for _, r := range t.Rules {
		for _, c := range r.Conds {
			if c.Op == ir.OpProg {
				if smtProve != nil && smtProve(cm, d, rep) {
					return
				}
				rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
					Message: "table not provable geometrically (expression cell Op=Prog) — residue not verified"})
				return
			}
		}
	}

	dims := make([]dim, len(t.Inputs))
	size := 1
	for j, col := range t.Inputs {
		dm := buildDim(col, t.Rules, j, cm.Domains[col])
		dims[j] = dm
		size *= len(dm.witnesses)
		if size > gridBudget {
			rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
				Message: fmt.Sprintf("verification grid too large (> %d) — not proven exhaustively", gridBudget)})
			return
		}
	}

	// Completeness (absence of gaps) only makes sense for single-hit policies:
	// for COLLECT / RULE ORDER, a region covered by 0 rules gives an expected empty result.
	singleHit := t.HitPolicy == ir.HitFirst || t.HitPolicy == ir.HitUnique ||
		t.HitPolicy == ir.HitAny || t.HitPolicy == ir.HitPriority

	everCovers := make([]bool, len(t.Rules))
	everFirst := make([]bool, len(t.Rules))
	allCovered := true
	gaps := 0
	conflicts := map[string]Finding{}

	// Subsumption matrix (bitset): subset[a] has bit b set if "A ⊆ B" remains possible. Initialized
	// all-true, we clear bit b as soon as a witness point covered by A is NOT covered by B.
	// Relevant for ANY / PRIORITY (overlap with identical outputs = redundant rule, not
	// reported elsewhere). UNIQUE = already a conflict; COLLECT/RULE ORDER = intended overlaps;
	// FIRST = already covered by dead-rule. uint64 bitset -> O(1) per covering rule and per point.
	trackSub := len(t.Rules) >= 2 && len(t.Rules) <= maxSubsumeRules &&
		(t.HitPolicy == ir.HitAny || t.HitPolicy == ir.HitPriority)
	var subset []uint64
	if trackSub {
		all := uint64(0)
		for i := range t.Rules {
			all |= 1 << uint(i)
		}
		subset = make([]uint64, len(t.Rules))
		for i := range subset {
			subset[i] = all
		}
	}

	err := eachPoint(dims, func(point []ir.Value) error {
		var covering []int
		for ri := range t.Rules {
			ok, err := ruleMatches(t.Rules[ri], point)
			if err != nil {
				return err
			}
			if ok {
				covering = append(covering, ri)
			}
		}
		if trackSub {
			var coverMask uint64
			for _, ri := range covering {
				coverMask |= 1 << uint(ri)
			}
			for _, a := range covering {
				subset[a] &= coverMask // any b not covered here is a witness that A⊄B
			}
		}
		if len(covering) == 0 {
			allCovered = false
			if singleHit && gaps < maxWitnessesPerKind {
				sev, msg := SevError, "case not covered by any rule (completeness gap)"
				if t.Default != nil {
					sev, msg = SevWarning, "case not covered by an explicit rule (caught by the `default` line)"
				}
				rep.add(Finding{Decision: d.Name, Kind: KindGap, Severity: sev, Message: msg,
					Witness: witnessMap(dims, point)})
			}
			if singleHit {
				gaps++
			}
			return nil
		}
		for _, ri := range covering {
			everCovers[ri] = true
		}
		everFirst[covering[0]] = true
		if len(covering) >= 2 {
			recordConflict(d.Name, t, covering, dims, point, conflicts)
		}
		return nil
	})
	if err != nil {
		rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
			Message: "analysis impossible (type inconsistency in a cell): " + err.Error()})
		return
	}

	if singleHit && gaps > maxWitnessesPerKind {
		rep.add(Finding{Decision: d.Name, Kind: KindGap, Severity: SevInfo,
			Message: fmt.Sprintf("(%d additional uncovered cases not listed)", gaps-maxWitnessesPerKind)})
	}
	for _, f := range sortedConflicts(conflicts) {
		rep.add(f)
	}

	// Dead / masked rules.
	for ri := range t.Rules {
		switch {
		case !everCovers[ri]:
			rep.add(Finding{Decision: d.Name, Kind: KindDeadRule, Severity: SevWarning, Rules: []int{ri + 1},
				Message: fmt.Sprintf("rule #%d never reachable (conditions impossible to satisfy)", ri+1)})
		case t.HitPolicy == ir.HitFirst && !everFirst[ri]:
			rep.add(Finding{Decision: d.Name, Kind: KindDeadRule, Severity: SevWarning, Rules: []int{ri + 1},
				Message: fmt.Sprintf("rule #%d masked: an earlier rule already covers all its cases (never the first to match)", ri+1)})
		}
	}

	// Subsumption: redundant rule (region included in another, identical output).
	if trackSub {
		reportSubsumption(d.Name, t, subset, everCovers, rep)
	}

	// Useless `default` line.
	if t.Default != nil && allCovered {
		rep.add(Finding{Decision: d.Name, Kind: KindUnreachableDefault, Severity: SevInfo,
			Message: "`default` line never used: the rules already cover all cases"})
	}
}

// reportSubsumption reports REDUNDANT rules: region included in another rule with
// an identical output (thus removable without changing the decision under ANY/PRIORITY). subset[a]
// carries bit b iff A⊆B on the grid (EXACT inclusion: the witnesses cover all
// atomic cells). Different outputs (dominance) are out of v2 scope (too
// policy-dependent) to avoid noise. Capped at maxWitnessesPerKind.
func reportSubsumption(dec string, t *ir.DecisionTable, subset []uint64, everCovers []bool, rep *Report) {
	reported := 0
	for a := range t.Rules {
		if !everCovers[a] {
			continue // never covered -> already reported as dead-rule
		}
		for b := range t.Rules {
			if a == b || subset[a]&(1<<uint(b)) == 0 || !everCovers[b] {
				continue
			}
			mutual := subset[b]&(1<<uint(a)) != 0 // identical regions
			if mutual && b < a {
				continue // mutual pair reported only once (a<b)
			}
			if !outputsEqual(t.Rules[a].Outputs, t.Rules[b].Outputs) {
				continue // different outputs: not "redundant"
			}
			if reported >= maxWitnessesPerKind {
				return
			}
			reported++
			msg := fmt.Sprintf("rule #%d redundant: region included in rule #%d with an identical output (removable)", a+1, b+1)
			if mutual {
				msg = fmt.Sprintf("rules #%d and #%d: identical regions and outputs (one is redundant)", a+1, b+1)
			}
			rep.add(Finding{Decision: dec, Kind: KindSubsumed, Severity: SevWarning, Rules: []int{a + 1, b + 1}, Message: msg})
		}
	}
}

func ruleMatches(r ir.Rule, point []ir.Value) (bool, error) {
	for j, c := range r.Conds {
		ok, err := ir.MatchCell(c, point[j])
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func recordConflict(dec string, t *ir.DecisionTable, covering []int, dims []dim, point []ir.Value, out map[string]Finding) {
	switch t.HitPolicy {
	case ir.HitUnique:
		out[ruleKey(covering)] = Finding{Decision: dec, Kind: KindConflict, Severity: SevError,
			Message: "hit policy UNIQUE: several rules overlap", Witness: witnessMap(dims, point), Rules: oneBased(covering)}
	case ir.HitAny:
		// Conflict only if the outputs diverge.
		ref := t.Rules[covering[0]].Outputs
		for _, ri := range covering[1:] {
			if !outputsEqual(t.Rules[ri].Outputs, ref) {
				out[ruleKey(covering)] = Finding{Decision: dec, Kind: KindConflict, Severity: SevError,
					Message: "hit policy ANY: overlapping rules with divergent outputs", Witness: witnessMap(dims, point), Rules: oneBased(covering)}
				return
			}
		}
	}
	// FIRST/PRIORITY/COLLECT/RULE ORDER: the overlap is resolved/expected -> no conflict.
}

func outputsEqual(a, b []ir.Value) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !ir.ValueEq(a[i], b[i]) {
			return false
		}
	}
	return true
}

// --- dimension construction ---

func buildDim(col string, rules []ir.Rule, idx int, dom ir.Domain) dim {
	var nums []*apd.Decimal
	strVals := map[string]bool{}
	hasNum, hasStr, hasBool := false, false, false

	var scan func(c ir.CellTest)
	scan = func(c ir.CellTest) {
		switch c.Op {
		case ir.OpEq, ir.OpNe:
			switch c.A.Tag {
			case ir.TagNumber:
				hasNum = true
				nums = append(nums, c.A.Num)
			case ir.TagString:
				hasStr = true
				strVals[c.A.Str] = true
			case ir.TagBool:
				hasBool = true
			}
		case ir.OpLt, ir.OpLe, ir.OpGt, ir.OpGe:
			hasNum = true
			nums = append(nums, c.A.Num)
		case ir.OpInRange:
			hasNum = true
			nums = append(nums, c.A.Num, c.B.Num)
		case ir.OpInSet:
			for _, sub := range c.Sub {
				scan(sub)
			}
		}
	}
	for _, r := range rules {
		scan(r.Conds[idx])
	}

	switch dom.Kind {
	case ir.DomNumeric:
		hasNum = true
		if !dom.LoInf {
			nums = append(nums, dom.Lo.Num)
		}
		if !dom.HiInf {
			nums = append(nums, dom.Hi.Num)
		}
	case ir.DomEnum:
		for _, v := range dom.Enum {
			switch v.Tag {
			case ir.TagString:
				hasStr = true
				strVals[v.Str] = true
			case ir.TagNumber:
				hasNum = true
				nums = append(nums, v.Num)
			case ir.TagBool:
				hasBool = true
			}
		}
	}

	switch {
	case hasNum:
		return dim{col: col, witnesses: numericWitnesses(nums, dom)}
	case hasBool:
		return dim{col: col, witnesses: []ir.Value{ir.Bool(false), ir.Bool(true)}}
	case hasStr:
		return dim{col: col, witnesses: discreteWitnesses(strVals, dom)}
	default:
		return dim{col: col, witnesses: []ir.Value{ir.Null()}} // free column (always Any)
	}
}

func numericWitnesses(cuts []*apd.Decimal, dom ir.Domain) []ir.Value {
	pts := sortUnique(cuts)
	if len(pts) == 0 {
		if dom.Kind == ir.DomNumeric && !dom.LoInf {
			return []ir.Value{ir.Num(dom.Lo.Num)}
		}
		return []ir.Value{ir.Num(decimal.FromInt(0))}
	}
	var cand []*apd.Decimal
	cand = append(cand, pts...)
	for i := 0; i+1 < len(pts); i++ {
		cand = append(cand, midpoint(pts[i], pts[i+1]))
	}
	cand = append(cand, sub1(pts[0]), add1(pts[len(pts)-1]))

	var out []ir.Value
	seen := map[string]bool{}
	for _, c := range cand {
		if !inNumericDomain(dom, c) {
			continue
		}
		k := c.Text('f')
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, ir.Num(c))
	}
	return out
}

func discreteWitnesses(strVals map[string]bool, dom ir.Domain) []ir.Value {
	var out []ir.Value
	if dom.Kind == ir.DomEnum {
		for _, v := range dom.Enum {
			if v.Tag == ir.TagString {
				out = append(out, v)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	keys := make([]string, 0, len(strVals))
	for k := range strVals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		out = append(out, ir.Str(k))
	}
	out = append(out, ir.Str("\x00__autre__")) // sentinel "any other value"
	return out
}

func inNumericDomain(dom ir.Domain, v *apd.Decimal) bool {
	if dom.Kind != ir.DomNumeric {
		return true
	}
	if !dom.LoInf {
		c := decimal.Cmp(v, dom.Lo.Num)
		if c < 0 || (c == 0 && dom.LoOpen) {
			return false
		}
	}
	if !dom.HiInf {
		c := decimal.Cmp(v, dom.Hi.Num)
		if c > 0 || (c == 0 && dom.HiOpen) {
			return false
		}
	}
	return true
}

// --- grid enumeration ---

func eachPoint(dims []dim, fn func([]ir.Value) error) error {
	if len(dims) == 0 {
		return nil
	}
	idx := make([]int, len(dims))
	point := make([]ir.Value, len(dims))
	for {
		for j := range dims {
			point[j] = dims[j].witnesses[idx[j]]
		}
		if err := fn(point); err != nil {
			return err
		}
		k := len(dims) - 1
		for k >= 0 {
			idx[k]++
			if idx[k] < len(dims[k].witnesses) {
				break
			}
			idx[k] = 0
			k--
		}
		if k < 0 {
			break
		}
	}
	return nil
}

// --- decimal helpers ---

func sortUnique(xs []*apd.Decimal) []*apd.Decimal {
	sort.Slice(xs, func(i, j int) bool { return decimal.Cmp(xs[i], xs[j]) < 0 })
	var out []*apd.Decimal
	for i, x := range xs {
		if i == 0 || decimal.Cmp(x, xs[i-1]) != 0 {
			out = append(out, x)
		}
	}
	return out
}

func midpoint(a, b *apd.Decimal) *apd.Decimal {
	sum := new(apd.Decimal)
	decimal.Ctx.Add(sum, a, b)
	mid := new(apd.Decimal)
	decimal.Ctx.Quo(mid, sum, decimal.FromInt(2))
	return mid
}

func sub1(a *apd.Decimal) *apd.Decimal {
	r := new(apd.Decimal)
	decimal.Ctx.Sub(r, a, decimal.FromInt(1))
	return r
}

func add1(a *apd.Decimal) *apd.Decimal {
	r := new(apd.Decimal)
	decimal.Ctx.Add(r, a, decimal.FromInt(1))
	return r
}

// --- formatage ---

func witnessMap(dims []dim, point []ir.Value) map[string]string {
	m := make(map[string]string, len(dims))
	for j, dm := range dims {
		m[dm.col] = valueStr(point[j])
	}
	return m
}

func valueStr(v ir.Value) string {
	switch v.Tag {
	case ir.TagNumber:
		r := new(apd.Decimal)
		r.Reduce(v.Num)
		return r.Text('f')
	case ir.TagString:
		if v.Str == "\x00__autre__" {
			return "<any other value>"
		}
		return fmt.Sprintf("%q", v.Str)
	case ir.TagBool:
		if v.Bool {
			return "true"
		}
		return "false"
	default:
		return "null"
	}
}

func oneBased(idx []int) []int {
	out := make([]int, len(idx))
	for i, v := range idx {
		out[i] = v + 1
	}
	return out
}

func ruleKey(idx []int) string {
	parts := make([]string, len(idx))
	for i, v := range idx {
		parts[i] = fmt.Sprint(v)
	}
	return strings.Join(parts, ",")
}

func sortedConflicts(m map[string]Finding) []Finding {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Finding, 0, len(m))
	for _, k := range keys {
		out = append(out, m[k])
	}
	return out
}
