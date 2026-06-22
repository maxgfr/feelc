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
