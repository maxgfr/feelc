package dmnxml_test

import (
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/dmnxml"
	"github.com/maxgfr/feelc/internal/engine"
)

const gradeDMN = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/" name="grade">
  <inputData id="i1" name="score"><variable typeRef="integer"/></inputData>
  <decision id="d1" name="grade">
    <variable typeRef="string"/>
    <informationRequirement><requiredInput href="#i1"/></informationRequirement>
    <decisionTable hitPolicy="FIRST">
      <input label="Score"><inputExpression typeRef="integer"><text>score</text></inputExpression></input>
      <output name="grade" typeRef="string"/>
      <rule><inputEntry><text>&lt; 50</text></inputEntry><outputEntry><text>"F"</text></outputEntry></rule>
      <rule><inputEntry><text>[50..80)</text></inputEntry><outputEntry><text>"B"</text></outputEntry></rule>
      <rule><inputEntry><text>-</text></inputEntry><outputEntry><text>"A"</text></outputEntry></rule>
    </decisionTable>
  </decision>
</definitions>`

// priorityDMN uses hitPolicy="PRIORITY" with <outputValues> declaring the priority order.
const priorityDMN = `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20191111/MODEL/" name="risk">
  <inputData id="i1" name="score"><variable typeRef="integer"/></inputData>
  <decision id="d1" name="verdict">
    <variable typeRef="string"/>
    <informationRequirement><requiredInput href="#i1"/></informationRequirement>
    <decisionTable hitPolicy="PRIORITY">
      <input label="Score"><inputExpression typeRef="integer"><text>score</text></inputExpression></input>
      <output name="verdict" typeRef="string"><outputValues><text>"reject","review","approve"</text></outputValues></output>
      <rule><inputEntry><text>&gt;= 0</text></inputEntry><outputEntry><text>"approve"</text></outputEntry></rule>
      <rule><inputEntry><text>&gt;= 700</text></inputEntry><outputEntry><text>"review"</text></outputEntry></rule>
      <rule><inputEntry><text>&lt; 600</text></inputEntry><outputEntry><text>"reject"</text></outputEntry></rule>
    </decisionTable>
  </decision>
</definitions>`

// TestImportPriorityFidelity: PRIORITY must import as `hit: priority` + a `priority:` line derived
// from <outputValues> (previously degraded to FIRST — DMN TCK fidelity gap, now closed).
func TestImportPriorityFidelity(t *testing.T) {
	rules, _, err := dmnxml.Import([]byte(priorityDMN))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules, "hit: priority") || !strings.Contains(rules, `priority: "reject"`) {
		t.Fatalf("PRIORITY not imported with a priority line:\n%s", rules)
	}
	for _, c := range []struct {
		score int
		want  string
	}{{500, "reject"}, {800, "review"}, {650, "approve"}} {
		got, err := engine.Run(rules, "verdict", map[string]any{"score": c.score})
		if err != nil {
			t.Fatalf("score=%d on imported PRIORITY DSL: %v\n%s", c.score, err, rules)
		}
		if got != c.want {
			t.Errorf("verdict(%d) = %v, expected %q", c.score, got, c.want)
		}
	}
}

// TestImportOutputOrderFidelity: OUTPUT ORDER must import as `hit: output order` + a priority line
// and return all matches ordered by output priority (previously unsupported — DMN TCK gap, now closed).
func TestImportOutputOrderFidelity(t *testing.T) {
	dmn := strings.Replace(priorityDMN, `hitPolicy="PRIORITY"`, `hitPolicy="OUTPUT ORDER"`, 1)
	rules, _, err := dmnxml.Import([]byte(dmn))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rules, "hit: output order") || !strings.Contains(rules, `priority: "reject"`) {
		t.Fatalf("OUTPUT ORDER not imported with a priority line:\n%s", rules)
	}
	got, err := engine.Run(rules, "verdict", map[string]any{"score": 800})
	if err != nil {
		t.Fatalf("output order on imported DSL: %v\n%s", err, rules)
	}
	xs, ok := got.([]any)
	if !ok || len(xs) != 2 || xs[0] != "review" || xs[1] != "approve" {
		t.Errorf("verdict(800) = %v, expected [review approve]", got)
	}
}

// Round-trip: DMN XML -> import -> DSL -> (parse+compile+execution) via engine.Run.
func TestImportRoundTrip(t *testing.T) {
	rules, warns, err := dmnxml.Import([]byte(gradeDMN))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Logf("warnings: %v", warns)
	}
	if !strings.Contains(rules, "decision grade : string") || !strings.Contains(rules, "hit: first") {
		t.Fatalf("unexpected generated DSL:\n%s", rules)
	}
	for _, c := range []struct {
		score int
		want  string
	}{{30, "F"}, {65, "B"}, {90, "A"}} {
		got, err := engine.Run(rules, "grade", map[string]any{"score": c.score})
		if err != nil {
			t.Fatalf("score=%d on imported DSL: %v\n%s", c.score, err, rules)
		}
		if got != c.want {
			t.Errorf("grade(%d) = %v, expected %q", c.score, got, c.want)
		}
	}
}
