package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/maxgfr/feelc/internal/genai"
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
	w.Header().Set("ETag", strconv.Quote(p.Hash)) // the project hash is its content identity (concurrency)
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

// handleModules returns the per-module summary (name, path, hash, blocker count, decision count). The
// list is always sorted by module name (project.Load sorts it), so optional ?limit / ?offset windows are
// deterministic. Absent both params it returns every module (back-compat); `total` is always included so a
// paginating client knows the full count. Invalid/negative params degrade to the defaults rather than 400.
func (s *Server) handleModules(w http.ResponseWriter, r *http.Request) {
	p := s.proj.Load()
	if p == nil {
		writeErr(w, http.StatusNotFound, "not running in project mode")
		return
	}
	all := moduleSummaries(p)
	total := len(all)
	resp := map[string]any{"total": total}
	q := r.URL.Query()
	if q.Has("limit") || q.Has("offset") {
		limit, offset := total, 0 // default limit = all remaining
		if v, err := strconv.Atoi(q.Get("offset")); err == nil && v > 0 {
			offset = v
		}
		if v, err := strconv.Atoi(q.Get("limit")); err == nil && v >= 0 {
			limit = v
		}
		lo := min(offset, total)
		hi := total
		if limit < total-lo { // only narrow the window when limit is below the remaining count
			hi = lo + limit // safe: limit < total-lo ≤ total, so lo+limit cannot overflow
		}
		all = all[lo:hi]
		resp["limit"] = limit
		resp["offset"] = offset
	}
	resp["modules"] = all
	writeJSON(w, http.StatusOK, resp)
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
	// The module's content hash is its ETag: a subsequent PUT/DELETE can send it as If-Match to detect a
	// concurrent edit (optimistic concurrency) instead of silently clobbering someone else's change.
	w.Header().Set("ETag", strconv.Quote(m.Hash))
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
	// Reuse the served project's already-compiled+verified modules for any candidate module whose source
	// is unchanged, so verifying an N-module candidate after a one-module edit recompiles only that module
	// (the AI/editor live-verify loop stays O(changed), not O(N)). nil base (single-file mode) ⇒ no reuse.
	p, err := project.CompileReusing(req.Name, req.Modules, s.proj.Load())
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

// handleProjectChat is the project-aware authoring boundary: it builds a lexically-retrieved context for
// the target module (its source + cross-module signatures, no embeddings) and asks the user's LLM for an
// updated module. Like /v1/chat, the engine never sees the LLM — the returned draft is then compiled,
// verified and (optionally) persisted via the deterministic endpoints. 404 outside project mode; 501 when
// no LLM is configured.
func (s *Server) handleProjectChat(w http.ResponseWriter, r *http.Request) {
	p := s.proj.Load()
	if p == nil {
		writeErr(w, http.StatusNotFound, "not running in project mode")
		return
	}
	defer r.Body.Close()
	var req struct {
		Messages []genai.Message `json:"messages"`
		Module   string          `json:"module"`
		LLM      genai.Config    `json:"llm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if len(req.Messages) == 0 {
		writeErr(w, http.StatusBadRequest, "`messages` required")
		return
	}
	prov, err := genai.Resolve(req.LLM)
	if errors.Is(err, genai.ErrNotConfigured) {
		writeErr(w, http.StatusNotImplemented, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	contextBlock := p.RetrieveContext(lastUserContent(req.Messages), req.Module, 0)
	system := genai.SystemPrompt + "\n\n---\n" + genai.ProjectEditPrompt + "\n\n---\n" + contextBlock
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	reply, err := prov.Chat(ctx, system, req.Messages)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "LLM call failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"message": reply,
		"rules":   extractRules(reply),
		"module":  req.Module,
		"context": contextBlock,
	})
}

// lastUserContent returns the content of the most recent user turn (the retrieval query).
func lastUserContent(msgs []genai.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			return msgs[i].Content
		}
	}
	if len(msgs) > 0 {
		return msgs[len(msgs)-1].Content
	}
	return ""
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
	name := r.PathValue("name")
	p, err := s.ws.PutModuleIfMatch(name, string(src), stripETagQuotes(r.Header.Get("If-Match")))
	if err != nil {
		if errors.Is(err, project.ErrPreconditionFailed) {
			writeErr(w, http.StatusPreconditionFailed, err.Error()) // 412: stale If-Match (concurrent edit)
			return
		}
		writeCompileErr(w, err) // 422 (structured for a diag.Error, else the link/validation message)
		return
	}
	s.PublishProject(p)
	setModuleETag(w, p, name)
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
	// If-None-Match: * means "create only if it does not already exist" — a stale create then 412s instead
	// of the generic "already exists" 422, matching the PUT/If-Match concurrency semantics.
	ifNoneStar := strings.TrimSpace(r.Header.Get("If-None-Match")) == "*"
	p, err := s.ws.CreateModuleIfNoneMatch(req.Name, req.Source, ifNoneStar)
	if err != nil {
		if errors.Is(err, project.ErrPreconditionFailed) {
			writeErr(w, http.StatusPreconditionFailed, err.Error()) // 412: module already exists (If-None-Match: *)
			return
		}
		writeCompileErr(w, err)
		return
	}
	s.PublishProject(p)
	setModuleETag(w, p, req.Name)
	writeProjectState(w, p)
}

// handleDeleteModule removes a module (rejected if another module's `uses` binding depends on it).
func (s *Server) handleDeleteModule(w http.ResponseWriter, r *http.Request) {
	if s.ws == nil {
		writeErr(w, http.StatusNotFound, "module editing requires project mode (serve --project)")
		return
	}
	p, err := s.ws.DeleteModuleIfMatch(r.PathValue("name"), stripETagQuotes(r.Header.Get("If-Match")))
	if err != nil {
		if errors.Is(err, project.ErrPreconditionFailed) {
			writeErr(w, http.StatusPreconditionFailed, err.Error()) // 412: stale If-Match (concurrent edit)
			return
		}
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	s.PublishProject(p)
	writeProjectState(w, p)
}

// stripETagQuotes normalizes an If-Match / If-None-Match header value into a bare hash: it trims
// surrounding whitespace, an optional weak-validator "W/" prefix, and the surrounding double quotes
// (RFC 7232 entity-tags are quoted). "" stays "" (no precondition).
func stripETagQuotes(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "W/")
	return strings.Trim(s, `"`)
}

// setModuleETag echoes the post-mutation ETag (the module's new content hash) so the client can chain its
// next edit's If-Match without an extra GET. No-op if the module is absent (e.g. it was just deleted).
func setModuleETag(w http.ResponseWriter, p *project.Project, name string) {
	if m, ok := p.Module(name); ok {
		w.Header().Set("ETag", strconv.Quote(m.Hash))
	}
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
