package service_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fence = "```"

// goodContent is a complete model (catch-all rule -> no gap) the stub LLM emits.
func goodContent() string {
	rules := `model "m" {}
input n : number in [0..100]
@source "policy section 1"
decision band : string {
  needs: n
  hit: first
  < 50 => "low"
  -    => "high"
}`
	return "Done.\n" + fence + "rules\n" + rules + "\n" + fence
}

// gappedContent omits the catch-all -> a completeness gap (SevError blocker) over [50..100].
func gappedContent() string {
	rules := `model "m" {}
input n : number in [0..100]
@source "policy section 1"
decision band : string {
  needs: n
  hit: first
  < 50 => "low"
}`
	return "Draft.\n" + fence + "rules\n" + rules + "\n" + fence
}

func openaiReply(content string) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"content": content}}},
	})
	return string(b)
}

// /v1/ingest: the LLM drafts a complete model on the first try -> the loop converges in one round
// and the decision->@source mapping is read back from the COMPILED model.
func TestIngestHappyPath(t *testing.T) {
	clearLLMEnv(t)
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, openaiReply(goodContent()))
	}))
	defer stub.Close()

	body, _ := json.Marshal(map[string]any{
		"source": "Section 1: band is low under 50, otherwise high.",
		"llm":    map[string]string{"provider": "openai", "baseURL": stub.URL, "model": "x", "apiKey": "k"},
	})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/ingest", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Rules     string `json:"rules"`
		Converged bool   `json:"converged"`
		Blockers  int    `json:"blockers"`
		Rounds    []any  `json:"rounds"`
		Mapping   []struct {
			Decision   string `json:"decision"`
			SourceSpan string `json:"sourceSpan"`
		} `json:"mapping"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Converged || resp.Blockers != 0 {
		t.Errorf("expected converged/0 blockers, got converged=%v blockers=%d", resp.Converged, resp.Blockers)
	}
	if len(resp.Rounds) != 1 {
		t.Errorf("expected 1 round, got %d", len(resp.Rounds))
	}
	if !strings.Contains(resp.Rules, "decision band") {
		t.Errorf("rules not captured: %q", resp.Rules)
	}
	if len(resp.Mapping) != 1 || resp.Mapping[0].Decision != "band" || resp.Mapping[0].SourceSpan != "policy section 1" {
		t.Errorf("mapping = %+v, want band -> policy section 1", resp.Mapping)
	}
}

// The first draft has a gap; the engine feeds the finding back; the second draft fixes it.
func TestIngestRepairLoop(t *testing.T) {
	clearLLMEnv(t)
	var bodies []string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			io.WriteString(w, openaiReply(gappedContent()))
		} else {
			io.WriteString(w, openaiReply(goodContent()))
		}
	}))
	defer stub.Close()

	body, _ := json.Marshal(map[string]any{
		"source":    "Section 1: band is low under 50, otherwise high.",
		"maxRounds": 3,
		"llm":       map[string]string{"provider": "openai", "baseURL": stub.URL, "model": "x", "apiKey": "k"},
	})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/ingest", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Converged bool `json:"converged"`
		Blockers  int  `json:"blockers"`
		Rounds    []struct {
			N        int `json:"n"`
			Blockers int `json:"blockers"`
		} `json:"rounds"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(bodies) != 2 {
		t.Fatalf("expected 2 LLM calls, got %d", len(bodies))
	}
	if !resp.Converged || resp.Blockers != 0 {
		t.Errorf("expected convergence after repair, got converged=%v blockers=%d", resp.Converged, resp.Blockers)
	}
	if len(resp.Rounds) != 2 || resp.Rounds[0].Blockers == 0 {
		t.Errorf("expected 2 rounds with first blocked, got %+v", resp.Rounds)
	}
	// the 2nd LLM call must carry the engine's deterministic feedback (the gap finding fed back).
	if !strings.Contains(strings.ToLower(bodies[1]), "gap") {
		t.Errorf("repair feedback (gap finding) not sent to the LLM: %s", bodies[1])
	}
}

// No LLM configured -> honest 501 (engine endpoints stay usable).
func TestIngestNoLLM(t *testing.T) {
	clearLLMEnv(t)
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/ingest", strings.NewReader(`{"source":"some policy","llm":{}}`)))
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("unconfigured LLM should be 501, got %d", rec.Code)
	}
}

// The LLM never converges -> the loop stops at maxRounds and returns the last draft + blockers.
func TestIngestMaxRounds(t *testing.T) {
	clearLLMEnv(t)
	calls := 0
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		io.WriteString(w, openaiReply(gappedContent()))
	}))
	defer stub.Close()

	body, _ := json.Marshal(map[string]any{
		"source":    "policy",
		"maxRounds": 2,
		"llm":       map[string]string{"provider": "openai", "baseURL": stub.URL, "model": "x", "apiKey": "k"},
	})
	rec := httptest.NewRecorder()
	emptyHandler().ServeHTTP(rec, httptest.NewRequest("POST", "/v1/ingest", strings.NewReader(string(body))))
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Converged bool  `json:"converged"`
		Blockers  int   `json:"blockers"`
		Rounds    []any `json:"rounds"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Converged || resp.Blockers == 0 {
		t.Errorf("expected non-convergence, got converged=%v blockers=%d", resp.Converged, resp.Blockers)
	}
	if len(resp.Rounds) != 2 || calls != 2 {
		t.Errorf("expected exactly 2 rounds/calls, got rounds=%d calls=%d", len(resp.Rounds), calls)
	}
}
