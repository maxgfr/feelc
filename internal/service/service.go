// Package service exposes the engine over HTTP (net/http ServeMux 1.22). Model reads are
// lock-free (per-request snapshot -> in-flight requests survive a hot-swap), per-request recover
// (a VM panic never kills the process), JSON audit of every decision.
package service

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	apd "github.com/cockroachdb/apd/v3"

	"github.com/maxgfr/feelc/internal/audit"
	"github.com/maxgfr/feelc/internal/check"
	"github.com/maxgfr/feelc/internal/diag"
	"github.com/maxgfr/feelc/internal/engine"
	"github.com/maxgfr/feelc/internal/explain"
	"github.com/maxgfr/feelc/internal/ir"
	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/registry"
)

// Server: the HTTP facade.
type Server struct {
	reg    *registry.Registry
	audit  *audit.Logger
	reload func() error // manual reload (nil if not available)
}

func New(reg *registry.Registry, log *audit.Logger, reload func() error) *Server {
	return &Server{reg: reg, audit: log, reload: reload}
}

// Handler builds the router (ServeMux 1.22, method+wildcard patterns).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/decisions/{key}", s.handleDecision)
	mux.HandleFunc("POST /v1/decisions/{key}/explain", s.handleExplain)
	mux.HandleFunc("POST /v1/evaluate", s.handleEvaluate)
	mux.HandleFunc("GET /v1/model", s.handleModel)
	mux.HandleFunc("GET /v1/source", s.handleSource)           // current .rules source (web editor)
	mux.HandleFunc("POST /v1/verify", s.handleVerifyCandidate) // verify a CANDIDATE source (without swap)
	mux.HandleFunc("POST /v1/check", s.handleCheckCandidate)   // check claims against a CANDIDATE source
	mux.HandleFunc("POST /v1/admin/reload", s.handleReload)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", s.handleReady)
	return corsMW(recoverMW(mux))
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
		writeErr(w, http.StatusBadRequest, "`decision` field required")
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
	rec.Output = jsonify(out) // clean audit trace (decimals as numbers, not "2E+1")
	s.audit.Log(rec)
	writeJSON(w, http.StatusOK, map[string]any{
		"decision":     decision,
		"output":       jsonify(out),
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
	type decInfo struct {
		Name      string   `json:"name"`
		Kind      string   `json:"kind"`
		HitPolicy string   `json:"hitPolicy,omitempty"`
		Deps      []string `json:"deps,omitempty"`
	}
	decisions := make([]decInfo, len(entry.Model.Decisions))
	for i := range entry.Model.Decisions {
		d := &entry.Model.Decisions[i]
		info := decInfo{Name: d.Name, Deps: d.Deps}
		if d.Kind == ir.KindTable && d.Table != nil {
			info.Kind = "table"
			info.HitPolicy = hitPolicyName(d.Table.HitPolicy)
		} else {
			info.Kind = "literal-expr"
		}
		decisions[i] = info
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": entry.Model.Name, "version": entry.Version, "hash": entry.Hash, "decisions": decisions,
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
	_, hash, rep, err := loader.Compile(src)
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
	cm, _, _, err := loader.Compile([]byte(doc.Rules))
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

func hitPolicyName(h ir.HitPolicy) string {
	switch h {
	case ir.HitUnique:
		return "unique"
	case ir.HitAny:
		return "any"
	case ir.HitFirst:
		return "first"
	case ir.HitPriority:
		return "priority"
	case ir.HitCollect:
		return "collect"
	case ir.HitRuleOrder:
		return "rule order"
	}
	return ""
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

// corsMW allows a frontend (web editor) to call the API from the browser (BYO key on the
// frontend, never a secret on the engine side). Origin `*`: dev/tooling usage.
func corsMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
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

// jsonify converts decimals (and recursively lists/contexts) to json.Number for clean
// numeric serialization.
func jsonify(v any) any {
	switch x := v.(type) {
	case *apd.Decimal:
		return json.Number(x.Text('f'))
	case []any:
		for i := range x {
			x[i] = jsonify(x[i])
		}
		return x
	case map[string]any:
		for k := range x {
			x[k] = jsonify(x[k])
		}
		return x
	default:
		return v
	}
}
