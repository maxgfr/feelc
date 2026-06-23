// Package genai is feelc's OPTIONAL authoring-time LLM layer: it turns a natural-language
// conversation into a `.rules` draft. It lives strictly at the HTTP/authoring boundary (used only
// by the serve UI's /v1/chat handler) — the engine (compiler, ir, vm, verify) never imports it and
// stays pure, deterministic and network-free. "AI writes, the engine executes."
//
// Bring-your-own-LLM: the provider/model/key come from the request (or env fallback). Two adapters,
// both stdlib net/http (no SDK): Anthropic (Messages API) and OpenAI-compatible (Chat Completions,
// covering OpenAI/OpenRouter/local LM Studio/Ollama). When nothing is configured, Resolve returns
// ErrNotConfigured and the engine remains fully usable (honest degradation, like the SMT backend).
package genai

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"
)

// SystemPrompt is the authoring guidance handed to the model. It is the single source of truth for
// what the LLM may emit; it must stay in sync with docs/feel-subset.md and skill/references/*.
//
//go:embed prompt/system.md
var SystemPrompt string

// ExplainPrompt narrates a deterministic decision trace in plain English (LLM off the execution
// path — it describes an already-computed result).
//
//go:embed prompt/explain.md
var ExplainPrompt string

// TestsPrompt drafts test claims for a model (then run through the deterministic /v1/check).
//
//go:embed prompt/tests.md
var TestsPrompt string

// IngestPrompt turns an arbitrary business specification into a `.rules` draft with @source
// traceability and drives the bounded auto-repair loop (it consumes the verifier's findings).
//
//go:embed prompt/ingest.md
var IngestPrompt string

// ProjectEditPrompt instructs the model to edit ONE module inside a multi-module project; the service
// appends the lexically-retrieved project context (target source + cross-module signatures) after it.
//
//go:embed prompt/project_edit.md
var ProjectEditPrompt string

// Message is one conversation turn (role "user" or "assistant").
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Config is the bring-your-own-LLM configuration supplied per request (with env fallback).
type Config struct {
	Provider string `json:"provider"` // "anthropic" (default) | "openai"
	BaseURL  string `json:"baseURL"`  // override (proxy / local / test); empty = provider default
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

// Provider is a chat-completion backend. system is the authoring prompt; msgs is the thread.
type Provider interface {
	Chat(ctx context.Context, system string, msgs []Message) (string, error)
}

// ErrNotConfigured signals that no usable LLM credentials were provided (request nor env). The
// service maps this to 501 so the AI feature degrades honestly while the engine keeps working.
var ErrNotConfigured = errors.New("LLM not configured: provide provider/model/apiKey in the request, or set ANTHROPIC_API_KEY / FEELC_LLM_API_KEY")

const (
	defaultAnthropicModel = "claude-sonnet-4-6"
	defaultOpenAIModel    = "gpt-4o"
)

// Resolve fills defaults from the environment and returns a ready Provider, or ErrNotConfigured.
// Env: FEELC_LLM_PROVIDER, FEELC_LLM_MODEL, FEELC_LLM_BASE_URL, FEELC_LLM_API_KEY, ANTHROPIC_API_KEY.
func Resolve(cfg Config) (Provider, error) {
	if cfg.Provider == "" {
		cfg.Provider = envOr("FEELC_LLM_PROVIDER", "anthropic")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = os.Getenv("FEELC_LLM_BASE_URL")
	}
	if cfg.APIKey == "" {
		cfg.APIKey = os.Getenv("FEELC_LLM_API_KEY")
	}
	if cfg.APIKey == "" && cfg.Provider == "anthropic" {
		cfg.APIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if cfg.Model == "" {
		cfg.Model = envOr("FEELC_LLM_MODEL", defaultModel(cfg.Provider))
	}
	if cfg.APIKey == "" {
		return nil, ErrNotConfigured
	}
	switch cfg.Provider {
	case "anthropic":
		return &anthropic{cfg: cfg, http: defaultHTTP()}, nil
	case "openai":
		return &openai{cfg: cfg, http: defaultHTTP()}, nil
	default:
		return nil, fmt.Errorf("unknown LLM provider %q (use \"anthropic\" or \"openai\")", cfg.Provider)
	}
}

func defaultModel(provider string) string {
	if provider == "openai" {
		return defaultOpenAIModel
	}
	return defaultAnthropicModel
}

func defaultHTTP() *http.Client { return &http.Client{Timeout: 120 * time.Second} }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
