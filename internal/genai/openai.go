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

// openai calls an OpenAI-compatible Chat Completions API (POST {base}/v1/chat/completions). This
// covers OpenAI, OpenRouter, and local servers (LM Studio, Ollama in OpenAI mode) via BaseURL. The
// system prompt is sent as the leading {role:"system"} message.
type openai struct {
	cfg  Config
	http *http.Client
}

func (o *openai) Chat(ctx context.Context, system string, msgs []Message) (string, error) {
	base := o.cfg.BaseURL
	if base == "" {
		base = "https://api.openai.com"
	}
	all := make([]Message, 0, len(msgs)+1)
	all = append(all, Message{Role: "system", Content: system})
	all = append(all, msgs...)
	payload := map[string]any{
		"model":       o.cfg.Model,
		"temperature": 0,
		"max_tokens":  4096, // bound output (parity with the Anthropic adapter); accepted by OpenAI-compatible servers
		"messages":    all,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("authorization", "Bearer "+o.cfg.APIKey)

	resp, err := o.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("openai-compatible API %d: %s", resp.StatusCode, snippet(raw))
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("openai-compatible: decoding response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai-compatible: empty choices")
	}
	return out.Choices[0].Message.Content, nil
}
