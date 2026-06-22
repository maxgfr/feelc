// Package tck exécute des cas du DMN TCK (Technology Compatibility Kit) contre feelc et rapporte
// un taux de conformité. Pipeline par modèle : .dmn -> dmnxml.Import -> dsl.Parse -> compiler.Compile,
// puis pour chaque <testCase>/<resultNode> : engine.Eval + comparaison via check.Equal (MÊME
// sémantique d'égalité décimale exacte que `feelc check`, zéro duplication).
//
// Dégradation HONNÊTE (jamais conformer en silence) : tout cas hors sous-ensemble est SKIPPÉ avec
// une raison (type TCK non supporté date/time/duration, Import bloquant, Compile/Eval en échec —
// ex: dépendance décision→décision non câblée par l'import). Le % de conformité = passed /
// (passed+failed) ; les skips sont comptés et listés à part (couverture honnête).
package tck

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dmnxml"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/engine"
)

// --- format <testCases> du DMN TCK ---

type tckSuite struct {
	XMLName xml.Name  `xml:"testCases"`
	Cases   []tckCase `xml:"testCase"`
}

type tckCase struct {
	ID      string      `xml:"id,attr"`
	Inputs  []tckNode   `xml:"inputNode"`
	Results []tckResult `xml:"resultNode"`
}

type tckNode struct {
	Name  string   `xml:"name,attr"`
	Value tckValue `xml:"value"`
}

type tckResult struct {
	Name     string   `xml:"name,attr"`
	Expected tckValue `xml:"expected>value"`
}

type tckValue struct {
	Type       string         `xml:"type,attr"` // xsi:type (ex: "xsd:integer")
	Nil        string         `xml:"nil,attr"`  // xsi:nil
	Text       string         `xml:",chardata"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"`
}

type tckList struct {
	Items []tckValue `xml:"value"`
}

type tckComponent struct {
	Name  string   `xml:"name,attr"`
	Value tckValue `xml:"value"`
}

// --- rapport ---

type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"
	Skipped Status = "skipped"
)

// CaseResult : un (modèle, testCase, decision).
type CaseResult struct {
	Model    string `json:"model"`
	Case     string `json:"case"`
	Decision string `json:"decision"`
	Status   Status `json:"status"`
	Reason   string `json:"reason,omitempty"`   // si skipped/fail
	Expected string `json:"expected,omitempty"` // si fail
	Got      string `json:"got,omitempty"`      // si fail
}

type Report struct {
	Cases   []CaseResult `json:"cases"`
	Passed  int          `json:"passed"`
	Failed  int          `json:"failed"`
	Skipped int          `json:"skipped"`
}

func (r *Report) add(c CaseResult) {
	r.Cases = append(r.Cases, c)
	switch c.Status {
	case Pass:
		r.Passed++
	case Fail:
		r.Failed++
	case Skipped:
		r.Skipped++
	}
}

// Conformance renvoie le % de conformité = passed / (passed+failed) (les skips ne comptent pas).
func (r *Report) Conformance() float64 {
	den := r.Passed + r.Failed
	if den == 0 {
		return 0
	}
	return 100 * float64(r.Passed) / float64(den)
}

// Run exécute toute la suite TCK d'un répertoire (récursif) et renvoie le rapport.
func Run(suiteDir string) (*Report, error) {
	rep := &Report{}
	var dmnFiles []string
	err := filepath.WalkDir(suiteDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, ".dmn") {
			dmnFiles = append(dmnFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(dmnFiles) // déterminisme
	for _, dmnPath := range dmnFiles {
		runModel(dmnPath, rep)
	}
	return rep, nil
}

func runModel(dmnPath string, rep *Report) {
	model := strings.TrimSuffix(filepath.Base(dmnPath), ".dmn")
	testFiles := findTestFiles(dmnPath)

	skipAll := func(reason string) {
		for _, tf := range testFiles {
			suite, err := loadSuite(tf)
			if err != nil {
				continue
			}
			for _, c := range suite.Cases {
				for _, rn := range c.Results {
					rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Skipped, Reason: reason})
				}
			}
		}
	}

	data, err := os.ReadFile(dmnPath)
	if err != nil {
		skipAll("lecture .dmn impossible: " + err.Error())
		return
	}
	rules, warns, err := dmnxml.Import(data)
	if err != nil {
		skipAll("import DMN: " + err.Error())
		return
	}
	if blocker := blockingWarn(warns); blocker != "" {
		skipAll("import bloquant: " + blocker)
		return
	}
	m, err := dsl.Parse(rules)
	if err != nil {
		skipAll("parse: " + err.Error())
		return
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		skipAll("compile: " + err.Error())
		return
	}

	for _, tf := range testFiles {
		suite, err := loadSuite(tf)
		if err != nil {
			continue
		}
		for _, c := range suite.Cases {
			inputs, skipReason := decodeInputs(c.Inputs)
			for _, rn := range c.Results {
				if skipReason != "" {
					rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Skipped, Reason: skipReason})
					continue
				}
				expect, err := decodeValue(rn.Expected)
				if err != nil {
					rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Skipped, Reason: "résultat: " + err.Error()})
					continue
				}
				got, err := engine.Eval(cm, rn.Name, inputs)
				if err != nil {
					// Distinguer une dépendance hors-périmètre (non câblée par l'import → SKIP honnête)
					// d'un VRAI bug d'exécution sur un modèle compilé (division par zéro, conflit de
					// hit policy…) qui est une NON-CONFORMITÉ et doit compter comme FAIL (jamais conformer
					// en silence en gonflant le %). (Revue adverse, Tranche 4.)
					if isUnwiredError(err) {
						rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Skipped,
							Reason: "dépendance DRG / variable non câblée par l'import: " + err.Error()})
					} else {
						rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Fail,
							Reason: "erreur d'évaluation", Expected: fmt.Sprint(expect), Got: "erreur: " + err.Error()})
					}
					continue
				}
				if check.Equal(expect, got) {
					rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Pass})
				} else {
					rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Fail,
						Expected: fmt.Sprint(expect), Got: fmt.Sprint(got)})
				}
			}
		}
	}
}

// findTestFiles renvoie les XML de cas du MODÈLE (convention TCK `<modèle>-test-*.xml`), pour ne
// PAS appliquer les cas d'un modèle à un autre dans un répertoire multi-modèles (revue adverse).
func findTestFiles(dmnPath string) []string {
	dir := filepath.Dir(dmnPath)
	base := strings.TrimSuffix(filepath.Base(dmnPath), ".dmn")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".xml") {
			continue
		}
		// Associé au modèle : `<base>-test*.xml` ou `<base>-<...>.xml` (le `-` évite le préfixe partiel).
		if strings.HasPrefix(n, base+"-") || strings.HasPrefix(n, base+"_") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// isUnwiredError distingue une erreur d'exécution due à une dépendance/variable NON câblée par
// l'import DMN (hors-périmètre → skip honnête) d'un vrai bug d'évaluation (→ fail).
func isUnwiredError(err error) bool {
	return strings.Contains(err.Error(), "inconnue") // "variable inconnue ..." / "décision inconnue ..."
}

func loadSuite(path string) (*tckSuite, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s tckSuite
	if err := xml.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func decodeInputs(nodes []tckNode) (map[string]any, string) {
	inputs := make(map[string]any, len(nodes))
	for _, n := range nodes {
		v, err := decodeValue(n.Value)
		if err != nil {
			return nil, fmt.Sprintf("entrée %q: %s", n.Name, err.Error())
		}
		inputs[n.Name] = v
	}
	return inputs, ""
}

// decodeValue convertit une valeur TCK en any JSON-ish. Les nombres restent en json.Number
// (exactitude décimale, cf. gotcha). Types temporels / function -> non supportés (skip).
func decodeValue(v tckValue) (any, error) {
	if strings.EqualFold(v.Nil, "true") {
		return nil, nil
	}
	if v.List != nil {
		out := make([]any, 0, len(v.List.Items))
		for _, it := range v.List.Items {
			e, err := decodeValue(it)
			if err != nil {
				return nil, err
			}
			out = append(out, e)
		}
		return out, nil
	}
	if len(v.Components) > 0 {
		m := make(map[string]any, len(v.Components))
		for _, c := range v.Components {
			e, err := decodeValue(c.Value)
			if err != nil {
				return nil, err
			}
			m[c.Name] = e
		}
		return m, nil
	}
	typ := localType(v.Type)
	switch typ {
	case "string":
		return v.Text, nil // NE PAS trimmer : un xsd:string peut porter des espaces significatifs
	case "":
		if strings.TrimSpace(v.Text) == "" {
			return nil, nil
		}
		return v.Text, nil
	case "boolean":
		b, err := strconv.ParseBool(strings.TrimSpace(v.Text))
		if err != nil {
			return nil, fmt.Errorf("booléen invalide %q", v.Text)
		}
		return b, nil
	case "integer", "int", "long", "short", "decimal", "double", "float":
		return json.Number(strings.TrimSpace(v.Text)), nil
	case "date", "time", "datetime", "duration", "dayTimeDuration", "yearMonthDuration", "function":
		return nil, fmt.Errorf("type TCK %q non supporté", typ)
	default:
		return nil, fmt.Errorf("type TCK %q non supporté", v.Type)
	}
}

// localType extrait le nom local d'un xsi:type (ex: "xsd:integer" -> "integer").
func localType(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndexByte(t, ':'); i >= 0 {
		t = t[i+1:]
	}
	return strings.ToLower(t)
}

// blockingWarn renvoie le 1er avertissement d'import structurellement bloquant (le reste est toléré).
func blockingWarn(warns []string) string {
	for _, w := range warns {
		if strings.Contains(w, "invalide") || strings.Contains(w, "ni table ni") {
			return w
		}
	}
	return ""
}
