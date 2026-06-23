package genai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// anthropic calls the Anthropic Messages API (POST {base}/v1/messages). temperature 0 for the most
// deterministic authoring possible (the LLM is still non-deterministic by nature — which is exactly
// why its output is then handed to the deterministic feelc oracle).
type anthropic struct {
	cfg  Config
	http *http.Client
}

const anthropicVersion = "2023-06-01"

func (a *anthropic) Chat(ctx context.Context, system string, msgs []Message) (string, error) {
	base := a.cfg.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	payload := map[string]any{
		"model":       a.cfg.Model,
		"max_tokens":  4096,
		"temperature": 0,
		"system":      system,
		"messages":    msgs, // {role,content} already matches the API shape
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", a.cfg.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	resp, err := a.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("anthropic API %d: %s", resp.StatusCode, snippet(raw))
	}
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("anthropic: decoding response: %w", err)
	}
	var b strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String(), nil
}

// snippet trims an error body so we never echo a huge (or secret-laden) payload back.
func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 300 {
		return s[:300] + "…"
	}
	return s
}
