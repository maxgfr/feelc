package service_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/project"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
)

// TestProjectEndpointsFeatureDetect confirms the project endpoints 404 in single-file mode (so the UI
// can feature-detect) and return the module list once a project is set.
func TestProjectEndpointsFeatureDetect(t *testing.T) {
	srv := service.New(registry.New(), nil, nil)
	h := srv.Handler()

	// Single-file mode: no project set yet → 404.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/project", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("GET /v1/project without a project: got %d, want 404", rec.Code)
	}

	// Load the credit example as a project and publish it.
	p, err := project.Load("../../examples/credit")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetProject(p)

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/project", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/project with a project: got %d, want 200", rec.Code)
	}
	var body struct {
		Name    string `json:"name"`
		Hash    string `json:"hash"`
		Modules []struct {
			Name     string `json:"name"`
			Blockers int    `json:"blockers"`
		} `json:"modules"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Name != "credit" {
		t.Errorf("project name = %q, want %q", body.Name, "credit")
	}
	if body.Hash != p.Hash {
		t.Errorf("project hash = %q, want %q", body.Hash, p.Hash)
	}
	if len(body.Modules) != 1 || body.Modules[0].Name != "credit" {
		t.Fatalf("modules = %+v, want one named credit", body.Modules)
	}

	// Module source round-trips.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/modules/credit/source", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/modules/credit/source: got %d, want 200", rec.Code)
	}
	if rec.Body.Len() == 0 {
		t.Error("module source is empty")
	}

	// Unknown module → 404.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/modules/nope/source", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /v1/modules/nope/source: got %d, want 404", rec.Code)
	}
}

// TestProjectChatRequiresProjectMode confirms /v1/project/chat 404s in single-file mode.
func TestProjectChatRequiresProjectMode(t *testing.T) {
	srv := service.New(registry.New(), nil, nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/project/chat",
		strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"module":"x"}`)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("POST /v1/project/chat without a project: got %d, want 404", rec.Code)
	}
}

// TestModuleEditingDisabledWithoutWorkspace confirms the safe-by-default posture: a project served
// WITHOUT a workspace (no --allow-edit) reports editable:false and 404s the write endpoints.
func TestModuleEditingDisabledWithoutWorkspace(t *testing.T) {
	srv := service.New(registry.New(), nil, nil)
	h := srv.Handler()
	p, err := project.Load("../../examples/credit")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetProject(p) // read-only: no SetWorkspace

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/project", nil))
	var body struct {
		Editable bool `json:"editable"`
	}
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body.Editable {
		t.Error("editable should be false without a workspace")
	}

	for _, tc := range []struct{ method, path, payload string }{
		{"PUT", "/v1/modules/credit/source", "model x {}"},
		{"POST", "/v1/modules", `{"name":"n","source":"model n {}"}`},
		{"DELETE", "/v1/modules/credit", ""},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.payload)))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s %s without --allow-edit: got %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

// TestProjectHealthAndVerifyEndpoints checks GET /v1/project/health on a clean project and the
// candidate POST /v1/project/verify path (a module with a completeness gap is reported as blocked).
func TestProjectHealthAndVerifyEndpoints(t *testing.T) {
	srv := service.New(registry.New(), nil, nil)
	h := srv.Handler()
	p, err := project.Load("../../examples/credit")
	if err != nil {
		t.Fatal(err)
	}
	srv.SetProject(p)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/project/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v1/project/health: got %d, want 200", rec.Code)
	}
	var health struct {
		Status   string `json:"status"`
		Blockers int    `json:"blockers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&health); err != nil {
		t.Fatal(err)
	}
	if health.Status != "clean" || health.Blockers != 0 {
		t.Errorf("credit project health = %+v, want clean/0", health)
	}

	// Candidate verify: a single module with an uncovered case → blocked.
	body := `{"name":"cand","modules":[{"name":"m","source":"model \"m\" {}\ninput s : number in [0..100]\ndecision g : string {\n needs: s\n hit: first\n >= 50 => \"pass\"\n}\n"}]}`
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/project/verify", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /v1/project/verify: got %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	var vr struct {
		Status   string `json:"status"`
		Blockers int    `json:"blockers"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&vr); err != nil {
		t.Fatal(err)
	}
	if vr.Blockers < 1 || vr.Status != "blocked" {
		t.Errorf("candidate verify = %+v, want blocked with >=1 blocker", vr)
	}
}
