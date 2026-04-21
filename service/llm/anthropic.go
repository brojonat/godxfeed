package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// AnthropicProvider extracts a FilterSpec via Anthropic's Messages API
// with forced tool_use. The "tool" is a single extract_filter tool
// whose input_schema is FilterSchemaJSON; tool_choice forces the model
// to call it, so we don't have to parse free-form text.
type AnthropicProvider struct {
	APIKey  string
	Model   string // e.g. "claude-opus-4-7"
	BaseURL string // defaults to https://api.anthropic.com
	HTTP    *http.Client
	Version string // anthropic-version header; defaults to "2023-06-01"
}

// NewAnthropicProvider is a convenience constructor — fills in the
// public defaults so callers typically just pass APIKey + Model.
func NewAnthropicProvider(apiKey, model string) *AnthropicProvider {
	return &AnthropicProvider{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://api.anthropic.com",
		HTTP:    &http.Client{Timeout: 30 * time.Second},
		Version: "2023-06-01",
	}
}

func (a *AnthropicProvider) Name() string { return "anthropic" }

func (a *AnthropicProvider) Extract(ctx context.Context, userText string, now time.Time) (FilterSpec, error) {
	if a.APIKey == "" {
		return FilterSpec{}, fmt.Errorf("anthropic: APIKey is required")
	}
	if a.Model == "" {
		return FilterSpec{}, fmt.Errorf("anthropic: Model is required")
	}
	base := a.BaseURL
	if base == "" {
		base = "https://api.anthropic.com"
	}
	version := a.Version
	if version == "" {
		version = "2023-06-01"
	}

	// Embed the schema as a parsed object — Anthropic validates shape
	// here, so passing the raw string would fail. One alloc.
	var schema map[string]any
	if err := json.Unmarshal([]byte(FilterSchemaJSON), &schema); err != nil {
		return FilterSpec{}, fmt.Errorf("anthropic: embed schema: %w", err)
	}

	req := map[string]any{
		"model":      a.Model,
		"max_tokens": 1024,
		"system":     SystemPrompt(now),
		"messages": []map[string]any{
			{"role": "user", "content": userText},
		},
		"tools": []map[string]any{{
			"name":         "extract_filter",
			"description":  "Emit a structured subscription filter derived from the user's request.",
			"input_schema": schema,
		}},
		// Force the model to call extract_filter — otherwise it might
		// answer in prose and we'd be back to text parsing.
		"tool_choice": map[string]any{"type": "tool", "name": "extract_filter"},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("anthropic: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/messages", bytes.NewReader(body))
	if err != nil {
		return FilterSpec{}, fmt.Errorf("anthropic: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-api-key", a.APIKey)
	httpReq.Header.Set("anthropic-version", version)

	cl := a.HTTP
	if cl == nil {
		cl = http.DefaultClient
	}
	res, err := cl.Do(httpReq)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("anthropic: do request: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("anthropic: read response: %w", err)
	}
	if res.StatusCode/100 != 2 {
		// Cap the echoed body — Anthropic errors include useful detail
		// but can be long; the cap keeps log lines manageable.
		return FilterSpec{}, fmt.Errorf("anthropic: status %d: %s", res.StatusCode, truncate(string(raw), 512))
	}

	var parsed anthropicResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return FilterSpec{}, fmt.Errorf("anthropic: parse response: %w", err)
	}
	for _, blk := range parsed.Content {
		if blk.Type == "tool_use" && blk.Name == "extract_filter" {
			if len(blk.Input) == 0 {
				return FilterSpec{}, fmt.Errorf("anthropic: tool_use block has empty input")
			}
			return parseFilterJSON(blk.Input)
		}
	}
	return FilterSpec{}, fmt.Errorf("anthropic: no extract_filter tool_use in response (stop_reason=%q)", parsed.StopReason)
}

// anthropicResponse is the minimal slice of the Messages API response
// we care about. Everything else (usage, id, model echo, …) is ignored
// so the parser stays tolerant of new fields.
type anthropicResponse struct {
	Content []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name,omitempty"`
		Input json.RawMessage `json:"input,omitempty"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
