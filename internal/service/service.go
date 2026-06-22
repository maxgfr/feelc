// Package service expose le moteur via HTTP (net/http ServeMux 1.22). Lectures du modèle
// lock-free (snapshot par requête -> les requêtes en vol survivent à un hot-swap), recover par
// requête (un panic VM ne tue jamais le process), audit JSON de chaque décision.
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

// Server : la façade HTTP.
type Server struct {
	reg    *registry.Registry
	audit  *audit.Logger
	reload func() error // reload manuel (nil si non disponible)
}

func New(reg *registry.Registry, log *audit.Logger, reload func() error) *Server {
	return &Server{reg: reg, audit: log, reload: reload}
}

// Handler construit le routeur (ServeMux 1.22, patterns méthode+wildcard).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/decisions/{key}", s.handleDecision)
	mux.HandleFunc("POST /v1/decisions/{key}/explain", s.handleExplain)
	mux.HandleFunc("POST /v1/evaluate", s.handleEvaluate)
	mux.HandleFunc("GET /v1/model", s.handleModel)
	mux.HandleFunc("GET /v1/source", s.handleSource)         // source .rules courante (éditeur web)
	mux.HandleFunc("POST /v1/verify", s.handleVerifyCandidate) // vérifie une source CANDIDATE (sans swap)
	mux.HandleFunc("POST /v1/check", s.handleCheckCandidate)   // check claims sur une source CANDIDATE
	mux.HandleFunc("POST /v1/admin/reload", s.handleReload)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /readyz", s.handleReady)
	return corsMW(recoverMW(mux))
}

func (s *Server) handleDecision(w http.ResponseWriter, r *http.Request) {
	inputs, err := decodeInputs(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "entrée JSON invalide: "+err.Error())
		return
	}
	s.decide(w, r.PathValue("key"), inputs)
}

// handleExplain renvoie la trace de justification d'une décision (règle gagnante + cellules).
// Snapshot du modèle courant (survit à un hot-swap), sous recoverMW comme les autres routes.
func (s *Server) handleExplain(w http.ResponseWriter, r *http.Request) {
	inputs, err := decodeInputs(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "entrée JSON invalide: "+err.Error())
		return
	}
	entry := s.reg.Current()
	if entry == nil {
		writeErr(w, http.StatusServiceUnavailable, "aucun modèle chargé")
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
		writeErr(w, http.StatusBadRequest, "corps JSON invalide: "+err.Error())
		return
	}
	decision, _ := body["decision"].(string)
	if decision == "" {
		writeErr(w, http.StatusBadRequest, "champ `decision` requis")
		return
	}
	input, _ := body["input"].(map[string]any)
	s.decide(w, decision, input)
}

func (s *Server) decide(w http.ResponseWriter, decision string, inputs map[string]any) {
	entry := s.reg.Current() // snapshot : la requête termine sur ce modèle même en cas de swap
	if entry == nil {
		writeErr(w, http.StatusServiceUnavailable, "aucun modèle chargé")
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
	rec.Output = jsonify(out) // trace d'audit propre (décimaux en nombres, pas "2E+1")
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
		writeErr(w, http.StatusServiceUnavailable, "aucun modèle chargé")
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

// handleSource renvoie la source .rules courante (éditeur web : LIRE le modèle servi).
func (s *Server) handleSource(w http.ResponseWriter, _ *http.Request) {
	entry := s.reg.Current()
	if entry == nil {
		writeErr(w, http.StatusServiceUnavailable, "aucun modèle chargé")
		return
	}
	if entry.Source == nil {
		writeErr(w, http.StatusNotFound, "source indisponible (modèle chargé sans source, ex: .ir.bin)")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(entry.Source)
}

// handleVerifyCandidate compile+vérifie une source CANDIDATE en mémoire, SANS swap (éditeur web :
// prévisualiser la vérification avant de publier). Body = source .rules brute.
func (s *Server) handleVerifyCandidate(w http.ResponseWriter, r *http.Request) {
	src, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		writeErr(w, http.StatusBadRequest, "lecture du corps: "+err.Error())
		return
	}
	_, hash, rep, err := loader.Compile(src)
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"hash": hash, "report": rep, "blockers": rep.Blockers()})
}

// handleCheckCandidate exécute des claims sur une source CANDIDATE en mémoire (sans swap).
// Body JSON = { "rules": "<source>", "claims": [ ... ] }.
func (s *Server) handleCheckCandidate(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // exactitude des nombres attendus dans les claims
	var doc struct {
		Rules  string        `json:"rules"`
		Claims []check.Claim `json:"claims"`
	}
	if err := dec.Decode(&doc); err != nil {
		writeErr(w, http.StatusBadRequest, "corps JSON invalide: "+err.Error())
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

// writeCompileErr rend une erreur de compilation : structurée (422 + {file,line,col,...}) si c'est
// un diag.Error, sinon message brut.
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
		writeErr(w, http.StatusServiceUnavailable, "pas de modèle valide chargé")
		return
	}
	w.Write([]byte("ready"))
}

func (s *Server) handleReload(w http.ResponseWriter, _ *http.Request) {
	if s.reload == nil {
		writeErr(w, http.StatusNotImplemented, "reload manuel non disponible")
		return
	}
	if err := s.reload(); err != nil {
		writeErr(w, http.StatusInternalServerError, "reload échoué: "+err.Error())
		return
	}
	s.handleModel(w, nil)
}

// corsMW autorise un front (éditeur web) à appeler l'API depuis le navigateur (BYO key côté
// front, jamais de secret côté moteur). Origine `*` : usage dev/outillage.
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

// recoverMW garantit qu'un panic (ex: dans la VM) renvoie 500 sans tuer le process.
func recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				writeErr(w, http.StatusInternalServerError, "panic interne")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func decodeInputs(r *http.Request) (map[string]any, error) {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	dec.UseNumber() // exactitude décimale des nombres d'entrée
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

// jsonify convertit les décimaux (et récursivement listes/contexts) en json.Number pour une
// sérialisation numérique propre.
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
