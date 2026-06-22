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
