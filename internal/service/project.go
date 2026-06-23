package service

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/maxgfr/feelc/internal/graph"
	"github.com/maxgfr/feelc/internal/project"
	"github.com/maxgfr/feelc/internal/verify"
)

// handleProject returns the served project's manifest summary + module list. 404 when not running in
// project mode — the UI uses this to feature-detect (single-file vs project layout).
func (s *Server) handleProject(w http.ResponseWriter, _ *http.Request) {
	p := s.proj.Load()
	if p == nil {
		writeErr(w, http.StatusNotFound, "not running in project mode")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name":     p.Manifest.Name,
		"version":  p.Manifest.Version,
		"hash":     p.Hash,
		"default":  p.Manifest.Default,
		"tags":     p.Manifest.Tags,
		"domains":  p.Manifest.Domains,
		"editable": s.ws != nil, // false unless serving with --allow-edit; the UI hides write controls
		"modules":  moduleSummaries(p),
	})
}

// handleModules returns the per-module summary (name, path, hash, blocker count, decision count).
func (s *Server) handleModules(w http.ResponseWriter, _ *http.Request) {
	p := s.proj.Load()
	if p == nil {
		writeErr(w, http.StatusNotFound, "not running in project mode")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"modules": moduleSummaries(p)})
}

// handleModuleSource returns one module's raw .rules source (web editor: READ a module).
func (s *Server) handleModuleSource(w http.ResponseWriter, r *http.Request) {
	p := s.proj.Load()
	if p == nil {
		writeErr(w, http.StatusNotFound, "not running in project mode")
		return
	}
	m, ok := p.Module(r.PathValue("name"))
	if !ok {
		writeErr(w, http.StatusNotFound, "no such module: "+r.PathValue("name"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(m.Source)
}

// handleProjectHealth returns the aggregated project verification report (per-module counts + status).
func (s *Server) handleProjectHealth(w http.ResponseWriter, _ *http.Request) {
	p := s.proj.Load()
	if p == nil {
		writeErr(w, http.StatusNotFound, "not running in project mode")
		return
	}
	rep := p.Health()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   rep.Status,
		"report":   rep,
		"blockers": rep.Totals.Blockers,
	})
}

// handleProjectVerify verifies a CANDIDATE multi-module project posted in the body WITHOUT swapping the
// served project (the multi-module analogue of POST /v1/verify). Body = { name, modules:[{name,source,uses}] }.
func (s *Server) handleProjectVerify(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Name    string                 `json:"name"`
		Modules []project.SourceModule `json:"modules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if len(req.Modules) == 0 {
		writeErr(w, http.StatusBadRequest, "`modules` required")
		return
	}
	p, err := project.Compile(req.Name, req.Modules)
	if err != nil {
		writeCompileErr(w, err) // structured 422 for a diag.Error, else 422 with the link error message
		return
	}
	rep := p.Health()
	writeJSON(w, http.StatusOK, map[string]any{
		"hash":     p.Hash,
		"status":   rep.Status,
		"report":   rep,
		"blockers": rep.Totals.Blockers,
	})
}

// handleProjectGraph renders the cross-module decision requirements graph of the merged model. With
// independent modules the result is disjoint per-module subgraphs; cross-module `uses` edges (a later
// slice) appear automatically because they are ordinary qualified Deps in the merged model.
func (s *Server) handleProjectGraph(w http.ResponseWriter, _ *http.Request) {
	p := s.proj.Load()
	if p == nil {
		writeErr(w, http.StatusNotFound, "not running in project mode")
		return
	}
	rep := verify.Verify(p.Merged)
	g := graph.Build(p.Merged, rep)
	writeJSON(w, http.StatusOK, map[string]any{
		"mermaid":  g.Mermaid(),
		"dot":      g.DOT(),
		"graph":    g,
		"findings": rep.Findings,
		"blockers": rep.Blockers(),
	})
}

// handlePutModule replaces a module's source on disk (server-side persistence) after the whole project
// re-links — the golden rule: an invalid edit is rejected and the current project is kept. Body = raw
// .rules source. Requires project mode.
func (s *Server) handlePutModule(w http.ResponseWriter, r *http.Request) {
	if s.ws == nil {
		writeErr(w, http.StatusNotFound, "module editing requires project mode (serve --project)")
		return
	}
	src, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "reading body: "+err.Error())
		return
	}
	p, err := s.ws.PutModule(r.PathValue("name"), string(src))
	if err != nil {
		writeCompileErr(w, err) // 422 (structured for a diag.Error, else the link/validation message)
		return
	}
	s.PublishProject(p)
	writeProjectState(w, p)
}

// handleCreateModule adds a new module. Body = { name, source }. Requires project mode.
func (s *Server) handleCreateModule(w http.ResponseWriter, r *http.Request) {
	if s.ws == nil {
		writeErr(w, http.StatusNotFound, "module editing requires project mode (serve --project)")
		return
	}
	defer r.Body.Close()
	var req struct {
		Name   string `json:"name"`
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if req.Name == "" {
		writeErr(w, http.StatusBadRequest, "`name` required")
		return
	}
	p, err := s.ws.CreateModule(req.Name, req.Source)
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	s.PublishProject(p)
	writeProjectState(w, p)
}

// handleDeleteModule removes a module (rejected if another module's `uses` binding depends on it).
func (s *Server) handleDeleteModule(w http.ResponseWriter, r *http.Request) {
	if s.ws == nil {
		writeErr(w, http.StatusNotFound, "module editing requires project mode (serve --project)")
		return
	}
	p, err := s.ws.DeleteModule(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.PublishProject(p)
	writeProjectState(w, p)
}

// writeProjectState returns the new project hash + aggregated health after a successful mutation.
func writeProjectState(w http.ResponseWriter, p *project.Project) {
	rep := p.Health()
	writeJSON(w, http.StatusOK, map[string]any{
		"hash":     p.Hash,
		"status":   rep.Status,
		"report":   rep,
		"blockers": rep.Totals.Blockers,
		"modules":  moduleSummaries(p),
	})
}

// moduleSummaries projects the modules into a JSON-friendly summary list.
func moduleSummaries(p *project.Project) []map[string]any {
	out := make([]map[string]any, len(p.Modules))
	for i, m := range p.Modules {
		blockers := 0
		if m.Report != nil {
			blockers = m.Report.Blockers()
		}
		out[i] = map[string]any{
			"name":      m.Name,
			"path":      m.Path,
			"hash":      m.Hash,
			"blockers":  blockers,
			"decisions": len(m.Model.Decisions),
		}
	}
	return out
}
