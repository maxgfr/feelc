package project

import (
	"fmt"
	"sort"

	"github.com/maxgfr/feelc/internal/verify"
)

// ModuleHealth is one module's verification summary (counts by finding kind + blockers).
type ModuleHealth struct {
	Module        string           `json:"module"`
	Hash          string           `json:"hash"`
	Decisions     int              `json:"decisions"`
	Gaps          int              `json:"gaps"`
	Conflicts     int              `json:"conflicts"`
	DeadRules     int              `json:"deadRules"`
	Subsumed      int              `json:"subsumed"`
	NotVerifiable int              `json:"notVerifiable"`
	Blockers      int              `json:"blockers"`
	Findings      []verify.Finding `json:"findings,omitempty"`
}

// Advisory is a project-level (cross-module) note that is informational, not a verification blocker.
type Advisory struct {
	Kind    string   `json:"kind"`
	Message string   `json:"message"`
	Modules []string `json:"modules,omitempty"`
}

// ProjectReport is the aggregated health of a project: per-module counts, totals, and cross-module
// advisories. Status is "blocked" (any error-severity finding), "warnings" (non-blocking findings), or
// "clean".
type ProjectReport struct {
	Status      string         `json:"status"`
	Modules     []ModuleHealth `json:"modules"`
	Totals      ModuleHealth   `json:"totals"`
	CrossModule []Advisory     `json:"crossModule,omitempty"`
}

// Health aggregates the per-module verification (computed at load time, stored on each Module) into a
// project-wide report. It reuses each module's verify.Report — no re-verification. Incremental reloads and
// candidate verification (CompileReusing) reuse unchanged modules' reports by source hash, so editing one
// module re-verifies only that module, not all N.
func (p *Project) Health() *ProjectReport {
	rep := &ProjectReport{Totals: ModuleHealth{Module: "(total)"}}
	anyWarning := false
	for _, m := range p.Modules {
		mh := ModuleHealth{Module: m.Name, Hash: m.Hash, Decisions: len(m.Model.Decisions)}
		if m.Report != nil {
			for _, f := range m.Report.Findings {
				if f.Severity == verify.SevWarning {
					anyWarning = true
				}
				switch f.Kind {
				case verify.KindGap:
					mh.Gaps++
				case verify.KindConflict:
					mh.Conflicts++
				case verify.KindDeadRule:
					mh.DeadRules++
				case verify.KindSubsumed:
					mh.Subsumed++
				case verify.KindNotVerifiable:
					mh.NotVerifiable++
				}
				if f.Severity == verify.SevError {
					mh.Blockers++
				}
			}
			mh.Findings = m.Report.Findings
		}
		rep.Modules = append(rep.Modules, mh)
		rep.Totals.Gaps += mh.Gaps
		rep.Totals.Conflicts += mh.Conflicts
		rep.Totals.DeadRules += mh.DeadRules
		rep.Totals.Subsumed += mh.Subsumed
		rep.Totals.NotVerifiable += mh.NotVerifiable
		rep.Totals.Blockers += mh.Blockers
		rep.Totals.Decisions += mh.Decisions
	}
	rep.CrossModule = p.crossModuleAdvisories()
	switch {
	case rep.Totals.Blockers > 0:
		rep.Status = "blocked"
	case anyWarning:
		rep.Status = "warnings"
	default:
		rep.Status = "clean" // no findings, or info-level only (e.g. unreachable-default)
	}
	return rep
}

// crossModuleAdvisories flags input names declared independently in more than one module: after the
// namespaced merge these are distinct (NOT shared), which is usually intended — but surfacing it helps a
// modeller who meant to wire them with a `uses` binding. Bound (aliased) inputs are excluded.
func (p *Project) crossModuleAdvisories() []Advisory {
	if len(p.Modules) < 2 {
		return nil
	}
	owners := map[string][]string{}
	for _, m := range p.Modules {
		for name := range m.Model.Inputs {
			if _, bound := m.Uses[name]; bound {
				continue
			}
			owners[name] = append(owners[name], m.Name)
		}
	}
	var out []Advisory
	for name, mods := range owners {
		if len(mods) > 1 {
			sort.Strings(mods)
			out = append(out, Advisory{
				Kind:    "shared-input-name",
				Message: fmt.Sprintf("input %q is declared independently in %d modules; these are namespaced and NOT shared (use a `uses` binding to share)", name, len(mods)),
				Modules: mods,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Message < out[j].Message })
	return out
}
