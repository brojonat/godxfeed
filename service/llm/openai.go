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

// OpenAIProvider extracts a FilterSpec via the Chat Completions API
// with response_format.json_schema in strict mode. No function-calling
// loop — the model returns a JSON string that matches the schema
// exactly, and we parse it directly.
type OpenAIProvider struct {
	APIKey  string
	Model   string // e.g. "gpt-4o", "gpt-4o-mini"
	BaseURL string // defaults to https://api.openai.com
	HTTP    *http.Client
}

func NewOpenAIProvider(apiKey, model string) *OpenAIProvider {
	return &OpenAIProvider{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://api.openai.com",
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (o *OpenAIProvider) Name() string { return "openai" }

func (o *OpenAIProvider) Extract(ctx context.Context, userText string, now time.Time) (FilterSpec, error) {
	if o.APIKey == "" {
		return FilterSpec{}, fmt.Errorf("openai: APIKey is required")
	}
	if o.Model == "" {
		return FilterSpec{}, fmt.Errorf("openai: Model is required")
	}
	base := o.BaseURL
	if base == "" {
		base = "https://api.openai.com"
	}

	var schema map[string]any
	if err := json.Unmarshal([]byte(FilterSchemaJSON), &schema); err != nil {
		return FilterSpec{}, fmt.Errorf("openai: embed schema: %w", err)
	}

	req := map[string]any{
		"model": o.Model,
		"messages": []map[string]any{
			{"role": "system", "content": SystemPrompt(now)},
			{"role": "user", "content": userText},
		},
		// Strict JSON-schema structured outputs — the server rejects
		// the response if it doesn't validate, so we never see prose
		// leaking in.
		"response_format": map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   "filter_spec",
				"schema": schema,
				"strict": true,
			},
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("openai: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return FilterSpec{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("authorization", "Bearer "+o.APIKey)

	cl := o.HTTP
	if cl == nil {
		cl = http.DefaultClient
	}
	res, err := cl.Do(httpReq)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("openai: do request: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("openai: read response: %w", err)
	}
	if res.StatusCode/100 != 2 {
		return FilterSpec{}, fmt.Errorf("openai: status %d: %s", res.StatusCode, truncate(string(raw), 512))
	}

	var parsed openAIResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return FilterSpec{}, fmt.Errorf("openai: parse response: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return FilterSpec{}, fmt.Errorf("openai: response has zero choices")
	}
	// Structured-output mode signals a refusal via `refusal` (non-empty
	// means the model declined). Treat as an error rather than an empty
	// spec — same reasoning as the Anthropic missing-tool-use case.
	if r := parsed.Choices[0].Message.Refusal; r != "" {
		return FilterSpec{}, fmt.Errorf("openai: model refused: %s", r)
	}
	content := parsed.Choices[0].Message.Content
	if content == "" {
		return FilterSpec{}, fmt.Errorf("openai: empty message content (finish_reason=%q)", parsed.Choices[0].FinishReason)
	}
	return parseFilterJSON([]byte(content))
}

type openAIResponse struct {
	Choices []struct {
		FinishReason string `json:"finish_reason"`
		Message      struct {
			Role    string `json:"role"`
			Content string `json:"content"`
			Refusal string `json:"refusal"`
		} `json:"message"`
	} `json:"choices"`
}
