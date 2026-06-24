package tck_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/tck"
)

// Regression (adversarial review): a runtime error on a COMPILED model (e.g. division by
// zero) is a NON-CONFORMANCE -> FAIL, not a skip (otherwise we silently inflate the %). Only
// dependencies not wired by the import are skipped.
func TestRunClassifiesEvalErrorAsFail(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dz")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	dmn := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/" name="dz">
  <decision name="r"><variable typeRef="number"/><literalExpression><text>1 / 0</text></literalExpression></decision>
</definitions>`
	test := `<?xml version="1.0" encoding="UTF-8"?>
<testCases xmlns="http://www.omg.org/spec/DMN/20160719/testcase" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <testCase id="c1"><resultNode name="r"><expected><value xsi:type="xsd:integer">5</value></expected></resultNode></testCase>
</testCases>`
	if err := os.WriteFile(filepath.Join(dir, "dz.dmn"), []byte(dmn), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dz-test-01.xml"), []byte(test), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err := tck.Run(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Failed != 1 || rep.Skipped != 0 {
		t.Errorf("division by zero = NON-CONFORMANCE: expected 1 fail / 0 skip, got %d fail / %d skip ; %+v",
			rep.Failed, rep.Skipped, rep.Cases)
	}
}

// TCK value decoder: decimal exactness (json.Number), basic types, and honest skip
// of unsupported types.
func TestRunGradeFixture(t *testing.T) {
	rep, err := tck.Run("../../testdata/dmn-tck")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 3 {
		t.Errorf("passed = %d, expected 3 (F/B/A) ; report=%+v", rep.Passed, rep.Cases)
	}
	if rep.Failed != 0 {
		t.Errorf("failed = %d, expected 0 ; report=%+v", rep.Failed, rep.Cases)
	}
	if rep.Skipped != 1 {
		t.Errorf("skipped = %d, expected 1 (date value) ; report=%+v", rep.Skipped, rep.Cases)
	}
	if c := rep.Conformance(); c != 100 {
		t.Errorf("conformance = %.1f, expected 100 (skips do not count)", c)
	}
	// The skip carries a reason (never silent).
	var skipReason string
	for _, c := range rep.Cases {
		if c.Status == tck.Skipped {
			skipReason = c.Reason
		}
	}
	if skipReason == "" || !strings.Contains(skipReason, "time") {
		t.Errorf("the skip must mention the unsupported type, got %q", skipReason)
	}
}

// Regression: a multi-output decision's expected result is `<expected><component>...</component>`
// (NOT wrapped in <value>); the runner must parse it (it previously read nil -> spuriously failed
// every multi-output / collect / ruleOrder case, badly under-reporting conformance).
func TestRunMultiOutputComponentExpected(t *testing.T) {
	rep, err := tck.Run("../../testdata/dmn-tck-multiout")
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 2 || rep.Failed != 0 {
		t.Errorf("multi-output <component> expected must parse + pass: got %d pass / %d fail ; report=%+v",
			rep.Passed, rep.Failed, rep.Cases)
	}
}

// The report is JSON-serializable (consumed by --json).
func TestReportJSON(t *testing.T) {
	rep, err := tck.Run("../../testdata/dmn-tck")
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"passed"`) || !strings.Contains(string(b), `"cases"`) {
		t.Errorf("unexpected JSON: %s", b)
	}
}
