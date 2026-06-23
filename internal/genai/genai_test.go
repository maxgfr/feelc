package genai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// clearEnv blanks every LLM env var so a developer's real key never leaks into the tests.
func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ANTHROPIC_API_KEY", "FEELC_LLM_API_KEY", "FEELC_LLM_PROVIDER", "FEELC_LLM_MODEL", "FEELC_LLM_BASE_URL"} {
		t.Setenv(k, "")
	}
}

func TestResolveNotConfigured(t *testing.T) {
	clearEnv(t)
	if _, err := Resolve(Config{}); err != ErrNotConfigured {
		t.Fatalf("Resolve with no key/env: got %v, want ErrNotConfigured", err)
	}
}

func TestResolveEnvFallback(t *testing.T) {
	clearEnv(t)
	t.Setenv("ANTHROPIC_API_KEY", "sk-from-env")
	p, err := Resolve(Config{}) // no provider/key in the request -> env supplies them
	if err != nil {
		t.Fatalf("Resolve env fallback: %v", err)
	}
	if _, ok := p.(*anthropic); !ok {
		t.Fatalf("default provider should be anthropic, got %T", p)
	}
}

func TestResolveUnknownProvider(t *testing.T) {
	clearEnv(t)
	if _, err := Resolve(Config{Provider: "frobnicator", APIKey: "k"}); err == nil {
		t.Fatalf("unknown provider must error")
	}
}

func TestAnthropicChat(t *testing.T) {
	clearEnv(t)
	var gotPath, gotKey, gotVersion, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotKey = r.Header.Get("x-api-key")
		gotVersion = r.Header.Get("anthropic-version")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("content-type", "application/json")
		io.WriteString(w, `{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}`)
	}))
	defer srv.Close()

	p, err := Resolve(Config{Provider: "anthropic", BaseURL: srv.URL, Model: "claude-x", APIKey: "sk-test"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Chat(context.Background(), "SYS-PROMPT", []Message{{Role: "user", Content: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello world" {
		t.Errorf("text blocks not concatenated: %q", out)
	}
	if gotPath != "/v1/messages" {
		t.Errorf("path = %q", gotPath)
	}
	if gotKey != "sk-test" || gotVersion != anthropicVersion {
		t.Errorf("headers: key=%q version=%q", gotKey, gotVersion)
	}
	if !strings.Contains(gotBody, `"system":"SYS-PROMPT"`) || !strings.Contains(gotBody, `"claude-x"`) {
		t.Errorf("body missing system/model: %s", gotBody)
	}
}

func TestOpenAIChat(t *testing.T) {
	clearEnv(t)
	var gotAuth string
	var msgs []Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		var body struct {
			Messages []Message `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		msgs = body.Messages
		io.WriteString(w, `{"choices":[{"message":{"content":"model \"m\" {}"}}]}`)
	}))
	defer srv.Close()

	p, err := Resolve(Config{Provider: "openai", BaseURL: srv.URL, Model: "gpt-x", APIKey: "key123"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Chat(context.Background(), "SYS", []Message{{Role: "user", Content: "make a model"}})
	if err != nil {
		t.Fatal(err)
	}
	if out != `model "m" {}` {
		t.Errorf("content = %q", out)
	}
	if gotAuth != "Bearer key123" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[0].Content != "SYS" {
		t.Errorf("system message must be prepended: %+v", msgs)
	}
}

func TestChatAPIError(t *testing.T) {
	clearEnv(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"bad key"}`)
	}))
	defer srv.Close()
	p, _ := Resolve(Config{Provider: "anthropic", BaseURL: srv.URL, APIKey: "x"})
	if _, err := p.Chat(context.Background(), "s", []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Fatalf("non-2xx must return an error")
	}
}

// TestSystemPromptSync guards against the embedded prompt drifting away from the canonical v2
// subset (docs/feel-subset.md, skill/references/*). If a construct is renamed there, update here too.
func TestSystemPromptSync(t *testing.T) {
	for _, tok := range []string{"```rules", "hit:", "collect sum", "round(x)", "default", "not(", "unique", "literal"} {
		if !strings.Contains(SystemPrompt, tok) {
			t.Errorf("system prompt missing expected token %q (drift from the canonical subset?)", tok)
		}
	}
}
