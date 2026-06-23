package service_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// /v1/trace compiles a candidate source and reports decision<->@source traceability + coverage.
func TestTraceCandidate(t *testing.T) {
	rules := `model "m" {}
input n : number in [0..100]
@source "policy section 1"
decision band : string {
  needs: n
  hit: first
  < 50 => "low"
  -    => "high"
}
decision twice : number = n * 2`
	spec := "Section 1: classify n into low or high.\n\nUnrelated paragraph about refunds within 30 days."
	body, _ := json.Marshal(map[string]any{"rules": rules, "spec": spec})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/trace", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var rep struct {
		Untraced []string `json:"untraced"`
		Coverage struct {
			Decisions        int `json:"decisions"`
			DecisionsSourced int `json:"decisionsSourced"`
			SpansTotal       int `json:"spansTotal"`
		} `json:"coverage"`
	}
	json.Unmarshal(rec.Body.Bytes(), &rep)
	if rep.Coverage.Decisions != 2 || rep.Coverage.DecisionsSourced != 1 {
		t.Errorf("coverage = %+v, want 2 decisions / 1 sourced", rep.Coverage)
	}
	if len(rep.Untraced) != 1 || rep.Untraced[0] != "twice" {
		t.Errorf("untraced = %v, want [twice]", rep.Untraced)
	}
	if rep.Coverage.SpansTotal != 2 {
		t.Errorf("spansTotal = %d, want 2", rep.Coverage.SpansTotal)
	}
}

func TestTraceCompileError(t *testing.T) {
	body, _ := json.Marshal(map[string]any{"rules": "not a valid model"})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/trace", strings.NewReader(string(body))))
	if rec.Code != 422 {
		t.Fatalf("compile error should be 422, got %d (%s)", rec.Code, rec.Body.String())
	}
}

// /v1/run with "full":true returns the WHOLE upstream DRG path (half then band), not just the goal.
func TestRunFullTrace(t *testing.T) {
	rules := `model "m" {}
input n : number in [0..100]
decision half : number = n / 2
decision band : string {
  needs: half
  hit: first
  < 25 => "low"
  -    => "high"
}`
	body, _ := json.Marshal(map[string]any{"rules": rules, "decision": "band", "input": map[string]any{"n": 10}, "full": true})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/run", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Output string `json:"output"`
		Trace  struct {
			Goal string `json:"goal"`
			Path []struct {
				Decision string `json:"decision"`
			} `json:"path"`
		} `json:"trace"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Output != "low" {
		t.Errorf("output=%q want low", resp.Output)
	}
	if resp.Trace.Goal != "band" || len(resp.Trace.Path) != 2 ||
		resp.Trace.Path[0].Decision != "half" || resp.Trace.Path[1].Decision != "band" {
		t.Errorf("full trace = %+v, want goal=band path=[half,band]", resp.Trace)
	}
}
