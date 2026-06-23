package service_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/maxgfr/feelc/internal/registry"
	"github.com/maxgfr/feelc/internal/service"
)

const sampleRules = "model \"m\" {}\ninput n : number in [0..100]\ndecision band : string {\n  needs: n\n  hit: first\n  < 50 => \"low\"\n  -    => \"high\"\n}\n"

func emptyHandler() http.Handler { return service.New(registry.New(), nil, nil).Handler() }

// clearLLMEnv blanks LLM env vars so a developer's real key never affects the tests.
func clearLLMEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ANTHROPIC_API_KEY", "FEELC_LLM_API_KEY", "FEELC_LLM_PROVIDER", "FEELC_LLM_MODEL", "FEELC_LLM_BASE_URL"} {
		t.Setenv(k, "")
	}
}

// /v1/run compiles a CANDIDATE source and evaluates it without a loaded model (editor preview).
func TestRunCandidate(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"rules": sampleRules, "decision": "band", "input": map[string]any{"n": 12}, "explain": true})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/run", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Output string         `json:"output"`
		Trace  map[string]any `json:"trace"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Output != "low" {
		t.Errorf("output=%q want low", resp.Output)
	}
	if resp.Trace == nil || resp.Trace["matched"] != true {
		t.Errorf("explain trace missing/not matched: %+v", resp.Trace)
	}
}

func TestRunCandidateCompileError(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"rules": "this is not valid", "decision": "band", "input": map[string]any{}})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/run", strings.NewReader(string(body))))
	if rec.Code != 422 {
		t.Fatalf("compile error should be 422, got %d (%s)", rec.Code, rec.Body.String())
	}
}

func TestRunMissingDecision(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"rules": sampleRules, "input": map[string]any{"n": 1}})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/run", strings.NewReader(string(body))))
	if rec.Code != 400 {
		t.Fatalf("missing decision should be 400, got %d", rec.Code)
	}
}

// /v1/chat forwards to the user's LLM (here an OpenAI-compatible stub) and extracts the rules block.
func TestChatExtractsRules(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"choices":[{"message":{"content":"Here you go:\n`+"```rules"+`\nmodel \"m\" {}\ndecision d : number = 1\n`+"```"+`"}}]}`)
	}))
	defer stub.Close()

	req := map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "make a model"}},
		"llm":      map[string]string{"provider": "openai", "baseURL": stub.URL, "model": "x", "apiKey": "k"},
	}
	body, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Message string `json:"message"`
		Rules   string `json:"rules"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp.Message, "Here you go") {
		t.Errorf("message not relayed: %q", resp.Message)
	}
	if !strings.Contains(resp.Rules, "model ") || !strings.Contains(resp.Rules, "decision d") {
		t.Errorf("rules block not extracted: %q", resp.Rules)
	}
}

func TestAssistExplain(t *testing.T) {
	clearLLMEnv(t)
	var gotBody string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		io.WriteString(w, `{"choices":[{"message":{"content":"The applicant was approved because the score exceeded 680."}}]}`)
	}))
	defer stub.Close()
	req := map[string]any{
		"task":    "explain",
		"payload": map[string]any{"trace": map[string]any{"decision": "eligibility", "output": true}},
		"llm":     map[string]string{"provider": "openai", "baseURL": stub.URL, "model": "x", "apiKey": "k"},
	}
	body, _ := json.Marshal(req)
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/assist", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct{ Message string }
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !strings.Contains(resp.Message, "approved because") {
		t.Errorf("message = %q", resp.Message)
	}
	if !strings.Contains(gotBody, "eligibility") { // the trace payload reached the model
		t.Errorf("payload not forwarded: %s", gotBody)
	}
}

func TestAssistUnknownTask(t *testing.T) {
	body := `{"task":"frobnicate","payload":{},"llm":{"provider":"openai","apiKey":"k"}}`
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/assist", strings.NewReader(body)))
	if rec.Code != 400 {
		t.Fatalf("unknown task should be 400, got %d", rec.Code)
	}
}

func TestAssistNotConfigured(t *testing.T) {
	clearLLMEnv(t)
	body := `{"task":"explain","payload":{},"llm":{}}`
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/assist", strings.NewReader(body)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unconfigured LLM should be 501, got %d", rec.Code)
	}
}

// With no provider/key in the request and none in the env, /v1/chat degrades honestly (501).
func TestChatNotConfigured(t *testing.T) {
	for _, k := range []string{"ANTHROPIC_API_KEY", "FEELC_LLM_API_KEY", "FEELC_LLM_PROVIDER", "FEELC_LLM_MODEL", "FEELC_LLM_BASE_URL"} {
		t.Setenv(k, "")
	}
	body := `{"messages":[{"role":"user","content":"hi"}],"llm":{}}`
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat", strings.NewReader(body)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unconfigured LLM should be 501, got %d", rec.Code)
	}
}

// The embedded UI is served only when EnableUI is set.
func TestUIServedWhenEnabled(t *testing.T) {
	srv := service.New(registry.New(), nil, nil)
	srv.EnableUI = true
	h := srv.Handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "feelc") {
		t.Fatalf("index not served: code=%d", rec.Code)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/app.js", nil))
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "use strict") {
		t.Fatalf("app.js not served: code=%d", rec.Code)
	}
}

func TestRequiredEndpoint(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"rules": sampleRules, "decision": "band"})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/required", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Inputs []struct {
			Name, Type, Domain string
		} `json:"inputs"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Inputs) != 1 || resp.Inputs[0].Name != "n" || resp.Inputs[0].Type != "number" {
		t.Fatalf("required inputs = %+v", resp.Inputs)
	}
	if resp.Inputs[0].Domain != "in [0..100]" {
		t.Errorf("domain = %q, want in [0..100]", resp.Inputs[0].Domain)
	}
}

func TestUIDisabledByDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 404 {
		t.Fatalf("root should be 404 when UI disabled, got %d", rec.Code)
	}
}
