package service

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/maxgfr/feelc/internal/loader"
	"github.com/maxgfr/feelc/internal/trace"
)

// handleTrace builds the SOURCE TRACEABILITY report of a CANDIDATE source (compile-from-body, no
// swap): which decisions cite which @source, which decisions cite none, and — when the raw spec
// text is supplied — best-effort coverage of which source paragraphs are referenced. LLM-free.
// Body = { "rules": "<source>", "spec": "<optional raw specification text>" }.
func (s *Server) handleTrace(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var doc struct {
		Rules string `json:"rules"`
		Spec  string `json:"spec"`
	}
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	cm, _, _, err := loader.Compile([]byte(doc.Rules))
	if err != nil {
		writeCompileErr(w, err)
		return
	}
	var rep *trace.Report
	if strings.TrimSpace(doc.Spec) != "" {
		rep = trace.BuildWithSource(cm, []byte(doc.Spec))
	} else {
		rep = trace.Build(cm)
	}
	writeJSON(w, http.StatusOK, rep)
}
