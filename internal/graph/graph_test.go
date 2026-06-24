package graph_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/graph"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/verify"
)

func build(t *testing.T, src string) *graph.Graph {
	t.Helper()
	cm, _, rep, err := loader.Compile([]byte(src))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	return graph.Build(cm, rep)
}

// A two-decision model yields the expected DRG: inputs feed dti, dti+inputs feed result.
func TestBuildEdges(t *testing.T) {
	g := build(t, `model "t" {}
input a : number >= 0
input b : number >= 0
decision ratio : number = a / b
decision band : string {
  needs: ratio
  hit: first
  < 1 => "low"
  -   => "high"
}`)
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.Name] = n
	}
	if nodes["a"].Kind != graph.KindInput || nodes["ratio"].Kind != graph.KindDecision {
		t.Fatalf("node kinds wrong: %+v", nodes)
	}
	if nodes["band"].HitPolicy != "first" {
		t.Errorf("band hit policy = %q", nodes["band"].HitPolicy)
	}
	want := map[string]bool{"a->ratio": false, "b->ratio": false, "ratio->band": false}
	for _, e := range g.Edges {
		key := nameByID(g, e.From) + "->" + nameByID(g, e.Into)
		if _, ok := want[key]; ok {
			want[key] = true
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("missing edge %s (edges=%+v)", k, g.Edges)
		}
	}
}

// A gap is overlaid on the decision node (severity + Mermaid class).
func TestFindingsOverlay(t *testing.T) {
	g := build(t, `model "t" {}
input n : number in [0..100]
decision d : string {
  needs: n
  hit: first
  < 50 => "lo"
}`)
	var d graph.Node
	for _, n := range g.Nodes {
		if n.Name == "d" {
			d = n
		}
	}
	if d.Severity != "error" || len(d.Findings) == 0 {
		t.Fatalf("expected an error overlay on d, got %+v", d)
	}
	mm := g.Mermaid()
	if !strings.Contains(mm, "flowchart LR") || !strings.Contains(mm, "class "+d.ID+" error") {
		t.Errorf("mermaid missing flowchart/class: %s", mm)
	}
	if !strings.Contains(g.DOT(), "digraph DRG") {
		t.Errorf("dot missing header")
	}
	var parsed graph.Graph
	if err := json.Unmarshal([]byte(g.JSON()), &parsed); err != nil || len(parsed.Nodes) == 0 {
		t.Errorf("json round-trip failed: %v", err)
	}
}

// TestMermaidModules: in project mode the Mermaid output groups nodes into per-module subgraphs, labels
// them by their local (unqualified) name, and draws cross-module dependencies as dashed edges.
func TestMermaidModules(t *testing.T) {
	g := &graph.Graph{
		Model: "lending",
		Nodes: []graph.Node{
			{ID: "n_kyc__passed", Name: "kyc__passed", Local: "passed", Module: "kyc", Kind: graph.KindDecision, DecisionKind: "expression"},
			{ID: "n_loan__approved", Name: "loan__approved", Local: "approved", Module: "loan", Kind: graph.KindDecision, HitPolicy: "first"},
		},
		Edges: []graph.Edge{{From: "n_kyc__passed", Into: "n_loan__approved", CrossModule: true}},
	}
	mm := g.Mermaid()
	for _, want := range []string{`subgraph n_kyc["kyc"]`, `subgraph n_loan["loan"]`, `n_kyc__passed -.-> n_loan__approved`, `["passed"]`} {
		if !strings.Contains(mm, want) {
			t.Errorf("mermaid missing %q in:\n%s", want, mm)
		}
	}
}

// build with a nil report must not panic (graph without verification).
func TestNilReport(t *testing.T) {
	cm, _, _, err := loader.Compile([]byte("model \"t\" {}\ninput n : number\ndecision d : number = n + 1"))
	if err != nil {
		t.Fatal(err)
	}
	g := graph.Build(cm, (*verify.Report)(nil))
	if len(g.Nodes) == 0 {
		t.Fatal("expected nodes")
	}
}

// RequiredInputs follows the DRG transitively (band needs ratio, which needs a and b).
func TestRequiredInputs(t *testing.T) {
	cm, _, _, err := loader.Compile([]byte(`model "t" {}
input a : number >= 0
input b : number >= 0
input unused : number >= 0
decision ratio : number = a / b
decision band : string {
  needs: ratio
  hit: first
  < 1 => "low"
  -   => "high"
}`))
	if err != nil {
		t.Fatal(err)
	}
	got, err := cm.RequiredInputs("band")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a", "b"} // sorted; transitive via ratio; "unused" excluded
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("RequiredInputs(band) = %v, want %v", got, want)
	}
	if _, err := cm.RequiredInputs("nope"); err == nil {
		t.Error("unknown decision must error")
	}
}

func nameByID(g *graph.Graph, id string) string {
	for _, n := range g.Nodes {
		if n.ID == id {
			return n.Name
		}
	}
	return id
}
