// Package verify est le différenciateur de feelc : il PROUVE des propriétés d'une table de
// décision par décomposition géométrique en cellules atomiques.
//
// Principe : sur chaque dimension (colonne d'entrée) on collecte les points de coupure (bornes
// des cellules + bornes du domaine). Le produit cartésien de points-témoins représentatifs
// couvre exhaustivement l'espace ; pour chaque point on compte les règles qui matchent (via la
// MÊME fonction ir.MatchCell que la VM -> preuve et exécution s'accordent). On en déduit :
//   - complétude : un point couvert par 0 règle = TROU (avec contre-exemple concret) ;
//   - conflits   : selon la hit policy (UNIQUE -> tout chevauchement ; ANY -> sorties divergentes) ;
//   - règles mortes / masquées (FIRST/PRIORITY) ; ligne `default` inutile ;
//   - subsumption (ANY/PRIORITY) : règle dont la région est incluse dans une autre avec la même
//     sortie -> REDONDANTE (supprimable), via une matrice d'inclusion (bitset) sur la même grille.
//
// Dégradation honnête : une table avec une cellule Op=Prog (non géométrique) ou une grille trop
// grande est signalée « non prouvée formellement » — jamais conformée en silence.
package verify

import (
	"fmt"
	"sort"
	"strings"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/decimal"
	"github.com/maxgfr/feelc/internal/ir"
)

// gridBudget borne la taille de la grille atomique (garde-fou anti-explosion).
const gridBudget = 500_000

// maxWitnessesPerKind limite le nombre de contre-exemples rapportés par catégorie.
const maxWitnessesPerKind = 5

type Kind string

const (
	KindGap                Kind = "gap"
	KindConflict           Kind = "conflict"
	KindDeadRule           Kind = "dead-rule"
	KindUnreachableDefault Kind = "unreachable-default"
	KindNotVerifiable      Kind = "not-verifiable"
	KindSubsumed           Kind = "subsumed" // règle dont la région est incluse dans une autre
)

// maxSubsumeRules : la matrice de subsumption tient dans un bitset uint64 jusqu'à 64 règles
// (la table 8×50 du bench y tient). Au-delà, l'analyse de subsumption est honnêtement omise.
const maxSubsumeRules = 64

type Severity string

const (
	SevError   Severity = "error"
	SevWarning Severity = "warning"
	SevInfo    Severity = "info"
)

// Finding : un diagnostic de vérification.
type Finding struct {
	Decision string            `json:"decision"`
	Kind     Kind              `json:"kind"`
	Severity Severity          `json:"severity"`
	Message  string            `json:"message"`
	Witness  map[string]string `json:"witness,omitempty"` // point-témoin (entrée -> valeur)
	Rules    []int             `json:"rules,omitempty"`   // règles concernées (1-based)
}

// Report : l'ensemble des diagnostics.
type Report struct {
	Findings []Finding `json:"findings"`
}

func (r *Report) add(f Finding) { r.Findings = append(r.Findings, f) }

// Blockers compte les findings bloquants (sévérité error).
func (r *Report) Blockers() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == SevError {
			n++
		}
	}
	return n
}

// Verify analyse toutes les décisions-tables d'un modèle compilé.
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

func verifyTable(cm *ir.CompiledModel, d *ir.Decision, rep *Report) {
	t := d.Table

	// Dégradation honnête : pas de cellule Op=Prog (non géométrique).
	for _, r := range t.Rules {
		for _, c := range r.Conds {
			if c.Op == ir.OpProg {
				rep.add(Finding{Decision: d.Name, Kind: KindNotVerifiable, Severity: SevWarning,
					Message: "table non prouvable géométriquement (cellule expression Op=Prog) — résidu non vérifié"})
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
				Message: fmt.Sprintf("grille de vérification trop grande (> %d) — non prouvée exhaustivement", gridBudget)})
			return
		}
	}

	// La complétude (absence de trou) n'a de sens que pour les politiques single-hit :
	// pour COLLECT / RULE ORDER, une région couverte par 0 règle donne un résultat vide attendu.
	singleHit := t.HitPolicy == ir.HitFirst || t.HitPolicy == ir.HitUnique ||
		t.HitPolicy == ir.HitAny || t.HitPolicy == ir.HitPriority

	everCovers := make([]bool, len(t.Rules))
	everFirst := make([]bool, len(t.Rules))
	allCovered := true
	gaps := 0
	conflicts := map[string]Finding{}

	// Matrice de subsumption (bitset) : subset[a] a le bit b si "A ⊆ B" reste possible. Initialisée
	// tout-à-vrai, on efface le bit b dès qu'un point-témoin couvert par A ne l'est PAS par B.
	// Pertinente pour ANY / PRIORITY (chevauchement à sorties identiques = règle redondante, non
	// signalé par ailleurs). UNIQUE = déjà conflit ; COLLECT/RULE ORDER = chevauchements voulus ;
	// FIRST = déjà couvert par dead-rule. Bitset uint64 -> O(1) par règle couvrante et par point.
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
				subset[a] &= coverMask // tout b non couvert ici est un témoin que A⊄B
			}
		}
		if len(covering) == 0 {
			allCovered = false
			if singleHit && gaps < maxWitnessesPerKind {
				sev, msg := SevError, "cas non couvert par aucune règle (trou de complétude)"
				if t.Default != nil {
					sev, msg = SevWarning, "cas non couvert par une règle explicite (rattrapé par la ligne `default`)"
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
			Message: "analyse impossible (incohérence de type dans une cellule): " + err.Error()})
		return
	}

	if singleHit && gaps > maxWitnessesPerKind {
		rep.add(Finding{Decision: d.Name, Kind: KindGap, Severity: SevInfo,
			Message: fmt.Sprintf("(%d cas non couverts supplémentaires non listés)", gaps-maxWitnessesPerKind)})
	}
	for _, f := range sortedConflicts(conflicts) {
		rep.add(f)
	}

	// Règles mortes / masquées.
	for ri := range t.Rules {
		switch {
		case !everCovers[ri]:
			rep.add(Finding{Decision: d.Name, Kind: KindDeadRule, Severity: SevWarning, Rules: []int{ri + 1},
				Message: fmt.Sprintf("règle #%d jamais atteignable (conditions impossibles à satisfaire)", ri+1)})
		case t.HitPolicy == ir.HitFirst && !everFirst[ri]:
			rep.add(Finding{Decision: d.Name, Kind: KindDeadRule, Severity: SevWarning, Rules: []int{ri + 1},
				Message: fmt.Sprintf("règle #%d masquée : une règle antérieure couvre déjà tous ses cas (jamais la première à matcher)", ri+1)})
		}
	}

	// Subsumption : règle redondante (région incluse dans une autre, sortie identique).
	if trackSub {
		reportSubsumption(d.Name, t, subset, everCovers, rep)
	}

	// Ligne `default` inutile.
	if t.Default != nil && allCovered {
		rep.add(Finding{Decision: d.Name, Kind: KindUnreachableDefault, Severity: SevInfo,
			Message: "ligne `default` jamais utilisée : les règles couvrent déjà tous les cas"})
	}
}

// reportSubsumption signale les règles REDONDANTES : région incluse dans une autre règle avec
// une sortie identique (donc supprimable sans changer la décision sous ANY/PRIORITY). subset[a]
// porte le bit b ssi A⊆B sur la grille (inclusion EXACTE : les témoins couvrent toutes les
// cellules atomiques). Les sorties différentes (dominance) sont hors périmètre v2 (trop
// dépendantes de la politique) pour éviter le bruit. Plafonné à maxWitnessesPerKind.
func reportSubsumption(dec string, t *ir.DecisionTable, subset []uint64, everCovers []bool, rep *Report) {
	reported := 0
	for a := range t.Rules {
		if !everCovers[a] {
			continue // jamais couverte -> déjà signalée dead-rule
		}
		for b := range t.Rules {
			if a == b || subset[a]&(1<<uint(b)) == 0 || !everCovers[b] {
				continue
			}
			mutual := subset[b]&(1<<uint(a)) != 0 // régions identiques
			if mutual && b < a {
				continue // paire mutuelle signalée une seule fois (a<b)
			}
			if !outputsEqual(t.Rules[a].Outputs, t.Rules[b].Outputs) {
				continue // sorties différentes : pas "redondant"
			}
			if reported >= maxWitnessesPerKind {
				return
			}
			reported++
			msg := fmt.Sprintf("règle #%d redondante : région incluse dans la règle #%d avec une sortie identique (supprimable)", a+1, b+1)
			if mutual {
				msg = fmt.Sprintf("règles #%d et #%d : régions et sorties identiques (l'une est redondante)", a+1, b+1)
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
			Message: "hit policy UNIQUE : plusieurs règles se chevauchent", Witness: witnessMap(dims, point), Rules: oneBased(covering)}
	case ir.HitAny:
		// Conflit seulement si les sorties divergent.
		ref := t.Rules[covering[0]].Outputs
		for _, ri := range covering[1:] {
			if !outputsEqual(t.Rules[ri].Outputs, ref) {
				out[ruleKey(covering)] = Finding{Decision: dec, Kind: KindConflict, Severity: SevError,
					Message: "hit policy ANY : règles en chevauchement avec sorties divergentes", Witness: witnessMap(dims, point), Rules: oneBased(covering)}
				return
			}
		}
	}
	// FIRST/PRIORITY/COLLECT/RULE ORDER : le chevauchement est résolu/attendu -> pas de conflit.
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

// --- construction des dimensions ---

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
		return dim{col: col, witnesses: []ir.Value{ir.Null()}} // colonne libre (toujours Any)
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
	out = append(out, ir.Str("\x00__autre__")) // sentinelle « toute autre valeur »
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

// --- énumération de la grille ---

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

// --- helpers décimaux ---

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
			return "<toute autre valeur>"
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
