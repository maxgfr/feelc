package service_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/compiler"
	"github.com/maxgfr/feelc/internal/dsl"
	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
)

func creditHandler(t *testing.T) http.Handler {
	t.Helper()
	b, err := os.ReadFile("../../examples/credit/credit.rules")
	if err != nil {
		t.Fatal(err)
	}
	m, err := dsl.Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	reg.Store(cm, "h1")
	return service.New(reg, nil, nil).Handler()
}

func TestServiceDecision(t *testing.T) {
	h := creditHandler(t)
	body := `{"credit_score":700,"annual_income":60000,"monthly_debt":1500,"age":40}`
	req := httptest.NewRequest("POST", "/v1/decisions/eligibility", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code = %d, attendu 200 ; corps: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Output       map[string]any `json:"output"`
		ModelVersion int64          `json:"modelVersion"`
		Hash         string         `json:"hash"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Output["eligible"] != true {
		t.Errorf("eligible = %v, attendu true ; sortie: %+v", resp.Output["eligible"], resp.Output)
	}
	if resp.ModelVersion != 1 || resp.Hash != "h1" {
		t.Errorf("version/hash = %d/%s, attendu 1/h1", resp.ModelVersion, resp.Hash)
	}
}

func TestServiceExplain(t *testing.T) {
	h := creditHandler(t)
	body := `{"credit_score":500,"annual_income":60000,"monthly_debt":1500,"age":40}`
	req := httptest.NewRequest("POST", "/v1/decisions/eligibility/explain", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("code = %d, attendu 200 ; corps: %s", rec.Code, rec.Body.String())
	}
	var tr struct {
		Matched   bool   `json:"matched"`
		RuleIndex int    `json:"ruleIndex"`
		HitPolicy string `json:"hitPolicy"`
		Cells     []struct {
			Input string `json:"input"`
			Value string `json:"value"`
		} `json:"cells"`
		Output map[string]any `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &tr); err != nil {
		t.Fatal(err)
	}
	if !tr.Matched || tr.RuleIndex != 1 {
		t.Errorf("attendu match règle #1, obtenu matched=%v ruleIndex=%d", tr.Matched, tr.RuleIndex)
	}
	if tr.Output["reason"] != "score insuffisant" {
		t.Errorf("reason = %v, attendu \"score insuffisant\"", tr.Output["reason"])
	}
	found := false
	for _, c := range tr.Cells {
		if c.Input == "credit_score" && c.Value == "500" {
			found = true
		}
	}
	if !found {
		t.Errorf("cellule justifiante credit_score=500 absente: %+v", tr.Cells)
	}
}

// GET /v1/model enrichi : chaque décision porte kind / hitPolicy / deps.
func TestServiceModelEnriched(t *testing.T) {
	h := creditHandler(t)
	req := httptest.NewRequest("GET", "/v1/model", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var resp struct {
		Decisions []struct {
			Name      string   `json:"name"`
			Kind      string   `json:"kind"`
			HitPolicy string   `json:"hitPolicy"`
			Deps      []string `json:"deps"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	var elig, dti bool
	for _, d := range resp.Decisions {
		if d.Name == "eligibility" {
			elig = true
			if d.Kind != "table" || d.HitPolicy != "first" || len(d.Deps) == 0 {
				t.Errorf("eligibility: kind/hit/deps = %s/%s/%v", d.Kind, d.HitPolicy, d.Deps)
			}
		}
		if d.Name == "dti" {
			dti = true
			if d.Kind != "literal-expr" {
				t.Errorf("dti: kind = %s, attendu literal-expr", d.Kind)
			}
		}
	}
	if !elig || !dti {
		t.Errorf("décisions eligibility/dti absentes: %+v", resp.Decisions)
	}
}

// POST /v1/verify : vérifie une source CANDIDATE (sans swap). Valide -> 200 + report ; invalide -> 422.
func TestServiceVerifyCandidate(t *testing.T) {
	h := creditHandler(t)
	good := `model "m" {}
input n : number in [0..10]
decision d : string {
  needs: n
  hit: first
  [0..5)  => "lo"
  [5..10] => "hi"
}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/verify", strings.NewReader(good)))
	if rec.Code != 200 {
		t.Fatalf("verify candidate valide: code %d, corps %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"report"`) {
		t.Errorf("report absent: %s", rec.Body.String())
	}
	// Source invalide -> 422 structuré.
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, httptest.NewRequest("POST", "/v1/verify", strings.NewReader("bogus")))
	if rec2.Code != 422 {
		t.Errorf("verify candidate invalide: code %d, attendu 422", rec2.Code)
	}
}

// POST /v1/check : claims sur une source candidate.
func TestServiceCheckCandidate(t *testing.T) {
	h := creditHandler(t)
	body := `{"rules":"model \"m\" {}\ninput n : number\ndecision d : string {\n  needs: n\n  hit: first\n  < 0 => \"neg\"\n  -   => \"pos\"\n}","claims":[{"decision":"d","input":{"n":-1},"expect":"neg"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/check", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("check candidate: code %d, corps %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "supported") {
		t.Errorf("claim attendu supported: %s", rec.Body.String())
	}
}

// GET /v1/source renvoie la source du modèle courant (si stockée).
func TestServiceSource(t *testing.T) {
	src := "model \"m\" {}\ninput n : number\ndecision d : number = n\n"
	m, err := dsl.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	cm, err := compiler.Compile(m)
	if err != nil {
		t.Fatal(err)
	}
	reg := registry.New()
	reg.StoreWithSource(cm, "h1", []byte(src))
	h := service.New(reg, nil, nil).Handler()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/source", nil))
	if rec.Code != 200 || rec.Body.String() != src {
		t.Errorf("source: code %d, corps %q", rec.Code, rec.Body.String())
	}
}

// CORS : preflight OPTIONS -> 204 + en-tête Access-Control-Allow-Origin.
func TestServiceCORS(t *testing.T) {
	h := creditHandler(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("OPTIONS", "/v1/verify", nil))
	if rec.Code != 204 {
		t.Errorf("preflight: code %d, attendu 204", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("en-tête CORS absent")
	}
}

func TestServiceBadInput(t *testing.T) {
	h := creditHandler(t)
	req := httptest.NewRequest("POST", "/v1/decisions/eligibility", strings.NewReader("pas du json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("code = %d, attendu 400", rec.Code)
	}
}

func TestServiceReadyWhenEmpty(t *testing.T) {
	srv := service.New(registry.New(), nil, nil) // aucun modèle
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest("GET", "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("readyz sans modèle = %d, attendu 503", rec.Code)
	}
}
