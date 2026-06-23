// Package service exposes the engine over HTTP (net/http ServeMux 1.22). Model reads are
// lock-free (per-request snapshot -> in-flight requests survive a hot-swap), per-request recover
// (a VM panic never kills the process), JSON audit of every decision.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maxgfr/feelc/internal/audit"
	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/genai"
	"github.com/maxgfr/feelc/internal/graph"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/modelinfo"
	"github.com/maxgfr/feelc/internal/project"
	"github.com/maxgfr/feelc/internal/registry"
)

// Server: the HTTP facade.
type Server struct {
	reg    *registry.Registry
	audit  *audit.Logger
	reload func() error // manual reload (nil if not available)

	// proj holds the current project when serving with `feelc serve --project` (nil in single-file
	// mode). The project endpoints feature-detect on it (404 when absent). Lock-free, swapped on reload.
	proj atomic.Pointer[project.Project]

	// ws is the mutable workspace backing the module-editing endpoints (nil unless serving a project
	// with editing enabled, i.e. `serve --project --allow-edit`).
	ws *project.Workspace

	// publishMu serializes PublishProject so the registry model and the project snapshot are swapped
	// together (no split-brain between a watcher reload and an HTTP mutation).
	publishMu sync.Mutex

	// cache memoizes candidate compilation by source hash (the editor recompiles the same buffer across
	// /v1/verify, /v1/run, /v1/graph, … — one compile, then cheap lookups).
	cache *loader.Cache

	// EnableUI serves the embedded authoring UI at `/` (set by `feelc serve --ui`).
	EnableUI bool
}

// maxRequestBody caps every request body (DoS backstop on the file-writing endpoints). 8 MiB is ample
// for a .rules source or a candidate project; larger bodies get 413 via http.MaxBytesReader.
const maxRequestBody = 8 << 20

func New(reg *registry.Registry, log *audit.Logger, reload func() error) *Server {
	return &Server{reg: reg, audit: log, reload: reload, cache: loader.NewCache(256)}
}

// SetProject publishes the current project for the project endpoints (nil clears it). Called by the
// serve command on initial load and after every project reload, mirroring the registry's atomic swap.
func (s *Server) SetProject(p *project.Project) { s.proj.Store(p) }

// CurrentProject returns the currently served project, or nil in single-file mode.
func (s *Server) CurrentProject() *project.Project { return s.proj.Load() }

// SetWorkspace attaches the mutable workspace that backs the module-editing endpoints.
func (s *Server) SetWorkspace(ws *project.Workspace) { s.ws = ws }

// PublishProject swaps the merged model into the registry and updates the project snapshot together
// under publishMu, so a concurrent watcher reload and HTTP mutation cannot leave the two out of sync.
// A redundant publish (same hash — e.g. the watcher re-reading our own atomic write) skips the registry
// version bump but still refreshes the project object. The single-module source is kept so GET /v1/source
// still works in that case.
func (s *Server) PublishProject(p *project.Project) {
	s.publishMu.Lock()
	defer s.publishMu.Unlock()
	if cur := s.reg.Current(); cur == nil || cur.Hash != p.Hash {
		var src []byte
		if len(p.Modules) == 1 {
			src = p.Modules[0].Source
		}
		s.reg.StoreWithSource(p.Merged, p.Hash, src)
	}
	s.proj.Store(p)
}

// Handler builds the router (ServeMux 1.22, method+wildcard patterns).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/decisions/{key}", s.handleDecision)
	mux.HandleFunc("POST /v1/decisions/{key}/explain", s.handleExplain)
	mux.HandleFunc("POST /v1/evaluate", s.handleEvaluate)
	mux.HandleFunc("GET /v1/model", s.handleModel)
	mux.HandleFunc("GET /v1/source", s.handleSource)                      // current .rules source (web editor)
	mux.HandleFunc("POST /v1/verify", s.handleVerifyCandidate)            // verify a CANDIDATE source (without swap)
	mux.HandleFunc("POST /v1/check", s.handleCheckCandidate)              // check claims against a CANDIDATE source
	mux.HandleFunc("POST /v1/chat", s.handleChat)                         // AI authoring: NL conversation -> .rules draft
	mux.HandleFunc("POST /v1/ingest", s.handleIngest)                     // AI ingestion: spec -> draft -> verify -> repair loop
	mux.HandleFunc("POST /v1/assist", s.handleAssist)                     // one-shot AI tasks: explain | tests
	mux.HandleFunc("POST /v1/run", s.handleRun)                           // evaluate a CANDIDATE source (without swap)
	mux.HandleFunc("POST /v1/graph", s.handleGraph)                       // DRG of a CANDIDATE source (mermaid/dot/json)
	mux.HandleFunc("POST /v1/trace", s.handleTrace)                       // source<->rule traceability + coverage of a CANDIDATE
	mux.HandleFunc("POST /v1/required", s.handleRequired)                 // inputs a decision transitively needs (question-flow)
	mux.HandleFunc("GET /v1/project", s.handleProject)                    // project manifest + module list (404 in single-file mode)
	mux.HandleFunc("GET /v1/project/health", s.handleProjectHealth)       // aggregated project verification report
	mux.HandleFunc("POST /v1/project/verify", s.handleProjectVerify)      // verify a CANDIDATE multi-module project (no swap)
	mux.HandleFunc("GET /v1/project/graph", s.handleProjectGraph)         // cross-module DRG of the merged model
	mux.HandleFunc("POST /v1/project/chat", s.handleProjectChat)          // project-aware AI authoring (lexical retrieval)
	mux.HandleFunc("GET /v1/modules", s.handleModules)                    // per-module summary (name, hash, blocker count)
	mux.HandleFunc("GET /v1/modules/{name}/source", s.handleModuleSource) // a module's .rules source
	mux.HandleFunc("PUT /v1/modules/{name}/source", s.handlePutModule)    // edit + persist a module (golden rule)
	mux.HandleFunc("POST /v1/modules", s.handleCreateModule)              // create a module {name, source}
	mux.HandleFunc("DELETE /v1/modules/{name}", s.handleDeleteModule)     // delete a module
	mux.HandleFunc("POST /v1/admin/reload", s.handleReload)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", s.handleReady)
	if s.EnableUI {
		mux.Handle("GET /", http.FileServerFS(uiFS())) // embedded authoring UI (catch-all, least specific)
	}
	return corsMW(recoverMW(bodyLimitMW(mux)))
}

// bodyLimitMW caps every request body so the file-writing endpoints (and the candidate compilers) cannot
// be made to buffer an unbounded upload into memory.
func bodyLimitMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxRequestBody {
			writeErr(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody) // also caps chunked / lying Content-Length
		}
		next.ServeHTTP(w, r)
	})
}

// handleChat is the OPTIONAL AI authoring boundary (ADR 0008): it forwards the conversation to the
// user's configured LLM (bring-your-own provider/model/key, with env fallback) and returns the
// assistant message plus the extracted `.rules` draft. The engine never sees the LLM — the draft is
// then compiled/verified/run deterministically via the other endpoints. 501 when no LLM is configured.
func (s *Server) handleChat(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Messages []genai.Message `json:"messages"`
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
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	reply, err := prov.Chat(ctx, genai.SystemPrompt, req.Messages)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "LLM call failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": reply, "rules": extractRules(reply)})
}

// handleRun evaluates a CANDIDATE source against a test input WITHOUT swapping the served model
// (same compile-from-body pattern as /v1/verify). Body = { rules, decision, input, explain? }.
func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // decimal exactness of input numbers
	var doc struct {
		Rules    string         `json:"rules"`
		Decision string         `json:"decision"`
		Input    map[string]any `json:"input"`
		Explain  bool           `json:"explain"`
		Full     bool           `json:"full"` // full = trace the whole upstream DRG path (ExplainFull)
	}
	if err := dec.Decode(&doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if doc.Decision == "" {
		writeErr(w, http.StatusBadRequest, "`decision` field required")
		return
	}
	cm, _, _, err := s.cache.Compile([]byte(doc.Rules))
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	out, err := engine.Eval(cm, doc.Decision, doc.Input)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	resp := map[string]any{"decision": doc.Decision, "output": modelinfo.JSONify(out)}
	switch {
	case doc.Full:
		if ft, err := explain.ExplainFull(cm, doc.Decision, doc.Input); err == nil {
			resp["trace"] = ft
		}
	case doc.Explain:
		if tr, err := explain.Explain(cm, doc.Decision, doc.Input); err == nil {
			resp["trace"] = tr
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGraph builds the decision requirements graph of a CANDIDATE source (compile-from-body, no
// swap) and returns all renderings at once so the UI can switch formats client-side.
func (s *Server) handleGraph(w http.ResponseWriter, r *http.Request) {
	src, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "reading body: "+err.Error())
		return
	}
	cm, _, rep, err := s.cache.Compile(src)
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	g := graph.Build(cm, rep)
	writeJSON(w, http.StatusOK, map[string]any{
		"mermaid":  g.Mermaid(),
		"dot":      g.DOT(),
		"graph":    g,
		"findings": rep.Findings,
		"blockers": rep.Blockers(),
	})
}

// handleAssist runs a one-shot AI task: "explain" narrates a deterministic trace in plain English,
// "tests" drafts test claims. The LLM never sees execution — explain describes an already-computed
// trace, and the drafted tests are then checked by the deterministic engine (/v1/check). 501 when no
// LLM is configured.
func (s *Server) handleAssist(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req struct {
		Task    string          `json:"task"`
		Payload json.RawMessage `json:"payload"`
		LLM     genai.Config    `json:"llm"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	var system string
	switch req.Task {
	case "explain":
		system = genai.ExplainPrompt
	case "tests":
		system = genai.TestsPrompt
	default:
		writeErr(w, http.StatusBadRequest, "unknown task "+req.Task+" (use \"explain\" or \"tests\")")
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
	ctx, cancel := context.WithTimeout(r.Context(), 120*time.Second)
	defer cancel()
	reply, err := prov.Chat(ctx, system, []genai.Message{{Role: "user", Content: string(req.Payload)}})
	if err != nil {
		writeErr(w, http.StatusBadGateway, "LLM call failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": reply, "rules": extractRules(reply)})
}

// handleRequired returns the inputs a decision transitively needs (backward reachability over the
// DRG), with their type/domain/metadata — the data the UI uses to build a minimal simulator form.
func (s *Server) handleRequired(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var doc struct {
		Rules    string `json:"rules"`
		Decision string `json:"decision"`
	}
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if doc.Decision == "" {
		writeErr(w, http.StatusBadRequest, "`decision` field required")
		return
	}
	cm, _, _, err := s.cache.Compile([]byte(doc.Rules))
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	req, err := cm.RequiredInputs(doc.Decision)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	byName := map[string]modelinfo.InputInfo{}
	for _, ii := range modelinfo.Inputs(cm) {
		byName[ii.Name] = ii
	}
	out := make([]modelinfo.InputInfo, 0, len(req))
	for _, n := range req {
		out = append(out, byName[n])
	}
	writeJSON(w, http.StatusOK, map[string]any{"decision": doc.Decision, "inputs": out})
}

// rulesBlock matches a ```rules fenced block (or a plain ``` block) in the assistant reply.
var rulesBlock = regexp.MustCompile("(?s)```(?:rules|feelc|dmn)?\\s*\\n(.*?)```")

// extractRules pulls the first fenced code block that looks like a feelc model out of the LLM reply
// ("" if none). It only returns a block that actually declares a model/decision so prose fences are
// ignored.
func extractRules(reply string) string {
	for _, m := range rulesBlock.FindAllStringSubmatch(reply, -1) {
		t := strings.TrimSpace(m[1])
		if strings.Contains(t, "model ") || strings.Contains(t, "decision ") {
			return t
		}
	}
	return ""
}

func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	inputs, err := decodeInputs(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON input: "+err.Error())
		return
	}
	s.decide(w, r.PathValue("key"), inputs)
}

// handleExplain returns the justification trace of a decision (winning rule + cells).
// Snapshot of the current model (survives a hot-swap), under recoverMW like the other routes.
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	inputs, err := decodeInputs(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON input: "+err.Error())
		return
	}
	entry := s.reg.Current()
	if entry == nil {
		writeErr(w, http.StatusServiceUnavailable, "no model loaded")
		return
	}
	tr, err := explain.Explain(entry.Model, r.PathValue("key"), inputs)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, tr)
}

func (s *Server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	body, err := decodeInputs(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	decision, _ := body["decision"].(string)
	if decision == "" {
		// In project mode a bare evaluate falls back to the manifest's `default` decision.
		if p := s.proj.Load(); p != nil {
			decision = p.Manifest.Default
		}
	}
	if decision == "" {
		writeErr(w, http.StatusBadRequest, "`decision` field required (or set a project `default`)")
		return
	}
	input, _ := body["input"].(map[string]any)
	s.decide(w, decision, input)
}

func (s *Server) decide(w http.ResponseWriter, decision string, inputs map[string]any) {
	entry := s.reg.Current() // snapshot: the request finishes on this model even on a swap
	if entry == nil {
		writeErr(w, http.StatusServiceUnavailable, "no model loaded")
		return
	}
	start := time.Now()
	out, err := engine.Eval(entry.Model, decision, inputs)
	dur := time.Since(start).Nanoseconds()

	rec := audit.Record{Decision: decision, Input: inputs, ModelVersion: entry.Version, Hash: entry.Hash, DurationNs: dur}
	if err != nil {
		rec.Error = err.Error()
		s.audit.Log(rec)
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	rec.Output = modelinfo.JSONify(out) // clean audit trace (decimals as numbers, not "2E+1")
	s.audit.Log(rec)
	writeJSON(w, http.StatusOK, map[string]any{
		"decision":     decision,
		"output":       modelinfo.JSONify(out),
		"modelVersion": entry.Version,
		"hash":         entry.Hash,
		"durationNs":   dur,
	})
}

func (s *Server) handleModel(w http.ResponseWriter, _ *http.Request) {
	entry := s.reg.Current()
	if entry == nil {
		writeErr(w, http.StatusServiceUnavailable, "no model loaded")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": entry.Model.Name, "version": entry.Version, "hash": entry.Hash,
		"inputs": modelinfo.Inputs(entry.Model), "decisions": modelinfo.Decisions(entry.Model),
	})
}

// handleSource returns the current .rules source (web editor: READ the served model).
func (s *Server) handleSource(w http.ResponseWriter, _ *http.Request) {
	entry := s.reg.Current()
	if entry == nil {
		writeErr(w, http.StatusServiceUnavailable, "no model loaded")
		return
	}
	if entry.Source == nil {
		writeErr(w, http.StatusNotFound, "source unavailable (model loaded without source, e.g. .ir.bin)")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(entry.Source)
}

// handleVerifyCandidate compiles+verifies a CANDIDATE source in memory, WITHOUT swap (web editor:
// preview the verification before publishing). Body = raw .rules source.
func (s *Server) handleVerifyCandidate(w http.ResponseWriter, r *http.Request) {
	src, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "reading body: "+err.Error())
		return
	}
	_, hash, rep, err := s.cache.Compile(src)
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "report": rep, "blockers": rep.Blockers()})
}

// handleCheckCandidate runs claims against a CANDIDATE source in memory (without swap).
// JSON body = { "rules": "<source>", "claims": [ ... ] }.
func (s *Server) handleCheckCandidate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // exactness of numbers expected in the claims
	var doc struct {
		Rules  string        `json:"rules"`
		Claims []check.Claim `json:"claims"`
	}
	if err := dec.Decode(&doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	cm, _, _, err := s.cache.Compile([]byte(doc.Rules))
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	rep := check.Check(cm, doc.Claims)
	writeJSON(w, http.StatusOK, map[string]any{"report": rep, "blockers": rep.Blockers()})
}

// writeCompileErr renders a compilation error: structured (422 + {file,line,col,...}) if it's
// a diag.Error, otherwise raw message.
func writeCompileErr(w http.ResponseWriter, err error) {
	var de *diag.Error
	if errors.As(err, &de) {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": de})
		return
	}
	writeErr(w, http.StatusUnprocessableEntity, err.Error())
}

func (s *Server) handleReady(w http.ResponseWriter, _ *http.Request) {
	if s.reg.Current() == nil {
		writeErr(w, http.StatusServiceUnavailable, "no valid model loaded")
		return
	}
	w.Write([]byte("ready"))
}

func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	if s.reload == nil {
		writeErr(w, http.StatusNotImplemented, "manual reload not available")
		return
	}
	if err := s.reload(); err != nil {
		writeErr(w, http.StatusInternalServerError, "reload failed: "+err.Error())
		return
	}
	s.handleModel(w, nil)
}

// corsMW supports a browser frontend WITHOUT exposing this local, secret-proxying API (the LLM
// endpoints forward the user's key) to arbitrary websites. The embedded UI is same-origin and needs
// no CORS headers at all; for a LOCAL cross-origin dev frontend we reflect the Origin only when it is
// a loopback address. An internet page therefore cannot read responses nor make preflighted POSTs.
func corsMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o := r.Header.Get("Origin"); isLoopbackOrigin(o) {
			w.Header().Set("Access-Control-Allow-Origin", o)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackOrigin reports whether an Origin header points at localhost/127.0.0.1/::1.
func isLoopbackOrigin(o string) bool {
	if o == "" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// recoverMW guarantees that a panic (e.g. in the VM) returns 500 without killing the process.
func recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeErr(w, http.StatusInternalServerError, "internal panic")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func decodeInputs(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // decimal exactness of input numbers
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"error": msg})
}
