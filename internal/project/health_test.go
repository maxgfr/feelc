package project

import (
	"strings"
	"testing"
)

func moduleHealth(rep *ProjectReport, name string) *ModuleHealth {
	for i := range rep.Modules {
		if rep.Modules[i].Module == name {
			return &rep.Modules[i]
		}
	}
	return nil
}

// TestProjectHealthAggregates loads a project with one clean module and one module that has a gap and a
// conflict, and confirms the per-module counts roll up and the overall status is "blocked".
func TestProjectHealthAggregates(t *testing.T) {
	p, err := Load("testdata/health")
	if err != nil {
		t.Fatal(err)
	}
	rep := p.Health()
	if rep.Status != "blocked" {
		t.Errorf("status = %q, want blocked", rep.Status)
	}

	bad := moduleHealth(rep, "bad")
	if bad == nil {
		t.Fatal("no `bad` module in the report")
	}
	if bad.Gaps < 1 {
		t.Errorf("bad.Gaps = %d, want >= 1", bad.Gaps)
	}
	if bad.Conflicts < 1 {
		t.Errorf("bad.Conflicts = %d, want >= 1", bad.Conflicts)
	}
	if bad.Blockers < 3 {
		t.Errorf("bad.Blockers = %d, want >= 3", bad.Blockers)
	}

	good := moduleHealth(rep, "good")
	if good == nil || good.Blockers != 0 {
		t.Errorf("good module should be clean, got %+v", good)
	}

	// Totals roll up across modules.
	if rep.Totals.Blockers != bad.Blockers+good.Blockers {
		t.Errorf("totals.Blockers = %d, want %d", rep.Totals.Blockers, bad.Blockers+good.Blockers)
	}
	if rep.Totals.Gaps < bad.Gaps {
		t.Errorf("totals.Gaps = %d, want >= %d", rep.Totals.Gaps, bad.Gaps)
	}
}

// TestSharedInputAdvisory confirms the cross-module advisory fires for an input name declared
// independently in two modules (alpha and beta both declare `age`).
func TestSharedInputAdvisory(t *testing.T) {
	p, err := Load("testdata/multi")
	if err != nil {
		t.Fatal(err)
	}
	rep := p.Health()
	var adv *Advisory
	for i := range rep.CrossModule {
		if rep.CrossModule[i].Kind == "shared-input-name" && strings.Contains(rep.CrossModule[i].Message, `"age"`) {
			adv = &rep.CrossModule[i]
		}
	}
	if adv == nil {
		t.Fatalf("expected a shared-input-name advisory for `age`, got %+v", rep.CrossModule)
	}
	if len(adv.Modules) != 2 {
		t.Errorf("advisory modules = %v, want [alpha beta]", adv.Modules)
	}
}

// TestCompileInMemoryProject covers the candidate-verification path (project.Compile, no filesystem),
// including cross-module aliasing.
func TestCompileInMemoryProject(t *testing.T) {
	kyc := SourceModule{Name: "kyc", Source: "model \"kyc\" {}\ninput score : number >= 0\ndecision passed : boolean = score >= 600\n"}
	loan := SourceModule{
		Name:   "loan",
		Source: "model \"loan\" {}\ninput ok : boolean\ndecision approved : boolean = ok\n",
		Uses:   map[string]string{"ok": "kyc.passed"},
	}
	p, err := Compile("lending", []SourceModule{loan, kyc}) // unsorted on purpose
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if _, ok := p.Merged.Inputs["loan__ok"]; ok {
		t.Error("bound input loan__ok should be omitted from merged inputs")
	}
	d, ok := p.Merged.Decision("loan__approved")
	if !ok || !containsStr(d.Deps, "kyc__passed") {
		t.Errorf("loan__approved should depend on kyc__passed; deps=%v", func() []string {
			if d != nil {
				return d.Deps
			}
			return nil
		}())
	}
	if p.Health().Status == "" {
		t.Error("health status should be set")
	}
}
