package project

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/ir"
)

// TestMergeQualifiesTableAndOpProg covers the highest-risk merge paths flagged by the audit:
// cloneTable (table input columns), cloneCellTest/cloneProg (Op=Prog cross-column Vars), and the
// preservation of context-output labels (Outputs are NOT references and must stay bare). The `pricing`
// module's `tier` table has a cross-column cell `>= threshold` (Op=Prog) and a context {label,surcharge}.
func TestMergeQualifiesTableAndOpProg(t *testing.T) {
	p, err := Load("testdata/tables")
	if err != nil {
		t.Fatal(err)
	}
	d, ok := p.Merged.Decision("pricing__tier")
	if !ok {
		t.Fatalf("merged model missing decision pricing__tier; decisions=%v", decisionNames(p.Merged))
	}
	if d.Table == nil {
		t.Fatal("pricing__tier is not a table")
	}

	// Table input columns are references → qualified.
	if got, want := d.Table.Inputs, []string{"pricing__amount", "pricing__threshold"}; !reflect.DeepEqual(got, want) {
		t.Errorf("table.Inputs = %v, want %v", got, want)
	}
	// Output labels are context field names → NOT references → left bare.
	if got, want := d.Table.Outputs, []string{"label", "surcharge"}; !reflect.DeepEqual(got, want) {
		t.Errorf("table.Outputs = %v, want %v (output labels must not be qualified)", got, want)
	}

	// The cross-column cell `>= threshold` is an Op=Prog whose Vars must be qualified.
	foundProg := false
	for _, r := range d.Table.Rules {
		for _, c := range r.Conds {
			if c.Op == ir.OpProg && c.Prog != nil {
				foundProg = true
				for _, v := range c.Prog.Vars {
					if v == "threshold" {
						t.Error("Op=Prog cell Var `threshold` left unqualified")
					}
				}
				if !containsStr(c.Prog.Vars, "pricing__threshold") {
					t.Errorf("Op=Prog cell Vars = %v, want to contain pricing__threshold", c.Prog.Vars)
				}
			}
		}
	}
	if !foundProg {
		t.Fatal("expected an Op=Prog cross-column cell in pricing__tier (fixture drift?)")
	}

	// End-to-end: evaluate the merged, qualified table and check the context result.
	out, err := engine.Eval(p.Merged, "pricing__tier", map[string]any{"pricing__amount": 100, "pricing__threshold": 50})
	if err != nil {
		t.Fatalf("eval pricing__tier: %v", err)
	}
	ctx, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected a context map, got %T (%v)", out, out)
	}
	if ctx["label"] != "high" {
		t.Errorf("tier(100,50).label = %v, want high", ctx["label"])
	}
	if fmt.Sprint(ctx["surcharge"]) != "1E+1" && fmt.Sprint(ctx["surcharge"]) != "10" {
		t.Errorf("tier(100,50).surcharge = %v, want 10", ctx["surcharge"])
	}

	// Low branch (default).
	out, _ = engine.Eval(p.Merged, "pricing__tier", map[string]any{"pricing__amount": 10, "pricing__threshold": 50})
	if ctx, _ := out.(map[string]any); ctx["label"] != "low" {
		t.Errorf("tier(10,50).label = %v, want low", ctx["label"])
	}

	// The standalone pricing model must still use the bare names (deep-copy held).
	pm, _ := p.Module("pricing")
	sd, _ := pm.Model.Decision("tier")
	if got := sd.Table.Inputs; !reflect.DeepEqual(got, []string{"amount", "threshold"}) {
		t.Errorf("standalone pricing.tier inputs mutated by merge: %v", got)
	}
}

// TestCloneCellTestQualifiesNestedSubProg directly exercises the OpInSet `Sub` recursion: a nested
// Op=Prog inside a set test must have its Vars qualified, and the original cell must be left untouched.
func TestCloneCellTestQualifiesNestedSubProg(t *testing.T) {
	res := func(local string) (string, error) { return "m" + sep + local, nil }
	c := ir.CellTest{
		Op: ir.OpInSet,
		Sub: []ir.CellTest{
			{Op: ir.OpProg, Prog: &ir.ExprProgram{Vars: []string{"threshold"}}},
			{Op: ir.OpEq, A: ir.Value{Tag: ir.TagNumber}}, // literal, no Vars to rewrite
		},
	}
	got, err := cloneCellTest(c, res)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sub[0].Prog.Vars[0] != "m__threshold" {
		t.Errorf("nested Sub Op=Prog Var not qualified: %v", got.Sub[0].Prog.Vars)
	}
	if c.Sub[0].Prog.Vars[0] != "threshold" {
		t.Error("original cell mutated by cloneCellTest (deep-copy violated)")
	}
}

func decisionNames(cm *ir.CompiledModel) []string {
	out := make([]string, len(cm.Decisions))
	for i := range cm.Decisions {
		out[i] = cm.Decisions[i].Name
	}
	return out
}

func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
