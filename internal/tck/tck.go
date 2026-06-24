// Package tck runs cases from the DMN TCK (Technology Compatibility Kit) against feelc and reports
// a conformance rate. Per-model pipeline: .dmn -> dmnxml.Import -> dsl.Parse -> compiler.Compile,
// then for each <testCase>/<resultNode>: engine.Eval + comparison via check.Equal (SAME
// exact-decimal equality semantics as `feelc check`, zero duplication).
//
// HONEST degradation (never conform silently): any case outside the subset is SKIPPED with
// a reason (unsupported TCK type date/time/duration, blocking Import, Compile/Eval failure —
// e.g. decision->decision dependency not wired by the import). The conformance % = passed /
// (passed+failed); skips are counted and listed separately (honest coverage).
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

// --- DMN TCK <testCases> format ---

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
	Name     string      `xml:"name,attr"`
	Expected tckExpected `xml:"expected"`
}

// tckExpected is the <expected> element, whose payload is EITHER a scalar <value>, OR a context
// (direct <component> children), OR a <list> — not always wrapped in <value> (so `expected>value`
// silently missed multi-output/collect/ruleOrder results, scoring them nil -> fail).
type tckExpected struct {
	Nil        string         `xml:"nil,attr"`
	Value      *tckValue      `xml:"value"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"`
}

func (e tckExpected) value() tckValue {
	if e.Value != nil {
		return *e.Value
	}
	return tckValue{Nil: e.Nil, List: e.List, Components: e.Components}
}

type tckValue struct {
	Type       string         `xml:"type,attr"` // xsi:type (e.g. "xsd:integer")
	Nil        string         `xml:"nil,attr"`  // xsi:nil
	Text       string         `xml:",chardata"`
	List       *tckList       `xml:"list"`
	Components []tckComponent `xml:"component"`
}

// tckList: a <list> whose entries are either <item> wrappers (each a scalar <value> or a context of
// <component>s) or, in some files, direct <value> children.
type tckList struct {
	Items  []tckExpected `xml:"item"`
	Values []tckValue    `xml:"value"`
}

type tckComponent struct {
	Name  string   `xml:"name,attr"`
	Value tckValue `xml:"value"`
}

// --- report ---

type Status string

const (
	Pass    Status = "pass"
	Fail    Status = "fail"
	Skipped Status = "skipped"
)

// CaseResult: a (model, testCase, decision).
type CaseResult struct {
	Model    string `json:"model"`
	Case     string `json:"case"`
	Decision string `json:"decision"`
	Status   Status `json:"status"`
	Reason   string `json:"reason,omitempty"`   // if skipped/fail
	Expected string `json:"expected,omitempty"` // if fail
	Got      string `json:"got,omitempty"`      // if fail
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

// Conformance returns the conformance % = passed / (passed+failed) (skips do not count).
func (r *Report) Conformance() float64 {
	den := r.Passed + r.Failed
	if den == 0 {
		return 0
	}
	return 100 * float64(r.Passed) / float64(den)
}

// Run executes the entire TCK suite of a directory (recursive) and returns the report.
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
	sort.Strings(dmnFiles) // determinism
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
		skipAll("cannot read .dmn: " + err.Error())
		return
	}
	rules, warns, err := dmnxml.Import(data)
	if err != nil {
		skipAll("DMN import: " + err.Error())
		return
	}
	if blocker := blockingWarn(warns); blocker != "" {
		skipAll("blocking import: " + blocker)
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
				expect, err := decodeValue(rn.Expected.value())
				if err != nil {
					rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Skipped, Reason: "result: " + err.Error()})
					continue
				}
				got, err := engine.Eval(cm, rn.Name, inputs)
				if err != nil {
					// Distinguish an out-of-scope dependency (not wired by the import -> honest SKIP)
					// from a REAL execution bug on a compiled model (division by zero, hit policy
					// conflict...) which is a NON-CONFORMANCE and must count as FAIL (never conform
					// silently by inflating the %). (Adversarial review, Slice 4.)
					if isUnwiredError(err) {
						rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Skipped,
							Reason: "DRG dependency / variable not wired by the import: " + err.Error()})
					} else {
						rep.add(CaseResult{Model: model, Case: c.ID, Decision: rn.Name, Status: Fail,
							Reason: "evaluation error", Expected: fmt.Sprint(expect), Got: "error: " + err.Error()})
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

// findTestFiles returns the case XML files of the MODEL (TCK convention `<model>-test-*.xml`), so as
// NOT to apply the cases of one model to another in a multi-model directory (adversarial review).
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
		// Associated with the model: `<base>-test*.xml` or `<base>-<...>.xml` (the `-` avoids the partial prefix).
		if strings.HasPrefix(n, base+"-") || strings.HasPrefix(n, base+"_") {
			out = append(out, filepath.Join(dir, n))
		}
	}
	sort.Strings(out)
	return out
}

// isUnwiredError distinguishes an execution error due to a dependency/variable NOT wired by the
// DMN import (out-of-scope -> honest skip) from a real evaluation bug (-> fail).
func isUnwiredError(err error) bool {
	return strings.Contains(err.Error(), "unknown") // "unknown variable ..." / "unknown decision ..."
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
			return nil, fmt.Sprintf("input %q: %s", n.Name, err.Error())
		}
		inputs[n.Name] = v
	}
	return inputs, ""
}

// decodeValue converts a TCK value into a JSON-ish any. Numbers stay as json.Number
// (decimal exactness, cf. gotcha). Temporal / function types -> not supported (skip).
func decodeValue(v tckValue) (any, error) {
	if strings.EqualFold(v.Nil, "true") {
		return nil, nil
	}
	if v.List != nil {
		items := v.List.Values
		for _, it := range v.List.Items {
			items = append(items, it.value())
		}
		out := make([]any, 0, len(items))
		for _, it := range items {
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
		return v.Text, nil // DO NOT trim: an xsd:string may carry significant spaces
	case "":
		if strings.TrimSpace(v.Text) == "" {
			return nil, nil
		}
		return v.Text, nil
	case "boolean":
		b, err := strconv.ParseBool(strings.TrimSpace(v.Text))
		if err != nil {
			return nil, fmt.Errorf("invalid boolean %q", v.Text)
		}
		return b, nil
	case "integer", "int", "long", "short", "decimal", "double", "float":
		return json.Number(strings.TrimSpace(v.Text)), nil
	case "date", "duration", "daytimeduration":
		// feelc renders date/duration as ISO strings (ADR 0014); compare textually.
		return strings.TrimSpace(v.Text), nil
	case "time", "datetime", "yearmonthduration", "function":
		return nil, fmt.Errorf("unsupported TCK type %q", typ)
	default:
		return nil, fmt.Errorf("unsupported TCK type %q", v.Type)
	}
}

// localType extracts the local name of an xsi:type (e.g. "xsd:integer" -> "integer").
func localType(t string) string {
	t = strings.TrimSpace(t)
	if i := strings.LastIndexByte(t, ':'); i >= 0 {
		t = t[i+1:]
	}
	return strings.ToLower(t)
}

// blockingWarn returns the 1st structurally blocking import warning (the rest is tolerated).
func blockingWarn(warns []string) string {
	for _, w := range warns {
		if strings.Contains(w, "invalide") || strings.Contains(w, "ni table ni") {
			return w
		}
	}
	return ""
}
