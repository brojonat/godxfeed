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

// GeminiProvider extracts a FilterSpec via Google's generateContent
// endpoint with response_mime_type=application/json + response_schema.
// Gemini's schema dialect is a subset of JSON Schema — in particular
// it doesn't support `type: [...]` unions, so the shared
// FilterSchemaJSON is adapted via geminiSchema() before send.
type GeminiProvider struct {
	APIKey  string
	Model   string // e.g. "gemini-2.5-pro", "gemini-2.0-flash"
	BaseURL string // defaults to https://generativelanguage.googleapis.com
	HTTP    *http.Client
}

func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	return &GeminiProvider{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://generativelanguage.googleapis.com",
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (g *GeminiProvider) Name() string { return "gemini" }

func (g *GeminiProvider) Extract(ctx context.Context, userText string, now time.Time) (FilterSpec, error) {
	if g.APIKey == "" {
		return FilterSpec{}, fmt.Errorf("gemini: APIKey is required")
	}
	if g.Model == "" {
		return FilterSpec{}, fmt.Errorf("gemini: Model is required")
	}
	base := g.BaseURL
	if base == "" {
		base = "https://generativelanguage.googleapis.com"
	}

	schema, err := geminiSchema()
	if err != nil {
		return FilterSpec{}, err
	}

	req := map[string]any{
		"systemInstruction": map[string]any{
			"parts": []map[string]any{{"text": SystemPrompt(now)}},
		},
		"contents": []map[string]any{{
			"role":  "user",
			"parts": []map[string]any{{"text": userText}},
		}},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"responseSchema":   schema,
		},
	}
	body, err := json.Marshal(req)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("gemini: marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/v1beta/models/%s:generateContent", base, g.Model)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return FilterSpec{}, fmt.Errorf("gemini: build request: %w", err)
	}
	httpReq.Header.Set("content-type", "application/json")
	httpReq.Header.Set("x-goog-api-key", g.APIKey)

	cl := g.HTTP
	if cl == nil {
		cl = http.DefaultClient
	}
	res, err := cl.Do(httpReq)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("gemini: do request: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return FilterSpec{}, fmt.Errorf("gemini: read response: %w", err)
	}
	if res.StatusCode/100 != 2 {
		return FilterSpec{}, fmt.Errorf("gemini: status %d: %s", res.StatusCode, truncate(string(raw), 512))
	}

	var parsed geminiResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return FilterSpec{}, fmt.Errorf("gemini: parse response: %w", err)
	}
	if len(parsed.Candidates) == 0 {
		// A blocked response ends up here — promptFeedback has the
		// reason. Surface it rather than falling back silently.
		if parsed.PromptFeedback.BlockReason != "" {
			return FilterSpec{}, fmt.Errorf("gemini: prompt blocked: %s", parsed.PromptFeedback.BlockReason)
		}
		return FilterSpec{}, fmt.Errorf("gemini: response has zero candidates")
	}
	cand := parsed.Candidates[0]
	if cand.FinishReason != "" && cand.FinishReason != "STOP" {
		return FilterSpec{}, fmt.Errorf("gemini: non-STOP finish reason %q", cand.FinishReason)
	}
	var text string
	for _, part := range cand.Content.Parts {
		if part.Text != "" {
			text += part.Text
		}
	}
	if text == "" {
		return FilterSpec{}, fmt.Errorf("gemini: empty text in candidate content")
	}
	return parseFilterJSON([]byte(text))
}

// geminiSchema adapts FilterSchemaJSON to Gemini's responseSchema
// dialect. Gemini accepts an OpenAPI-ish subset that disallows
// `type: [...]` unions; nullable fields use `nullable: true` instead.
// The function walks the parsed schema and rewrites exactly that
// construct — anything else passes through untouched.
func geminiSchema() (map[string]any, error) {
	var schema map[string]any
	if err := json.Unmarshal([]byte(FilterSchemaJSON), &schema); err != nil {
		return nil, fmt.Errorf("gemini: parse shared schema: %w", err)
	}
	normalizeForGemini(schema)
	return schema, nil
}

func normalizeForGemini(node map[string]any) {
	// Rewrite `type: ["X", "null"]` → `type: "X", nullable: true`.
	if t, ok := node["type"].([]any); ok {
		var primary string
		var nullable bool
		for _, v := range t {
			s, _ := v.(string)
			if s == "null" {
				nullable = true
				continue
			}
			if primary == "" {
				primary = s
			}
		}
		if primary != "" {
			node["type"] = primary
		}
		if nullable {
			node["nullable"] = true
		}
	}
	// Recurse into any nested schema objects.
	for _, key := range []string{"properties", "items"} {
		switch v := node[key].(type) {
		case map[string]any:
			for _, child := range v {
				if m, ok := child.(map[string]any); ok {
					normalizeForGemini(m)
				}
			}
			// items (when singular object) is also handled here for
			// that case via the inner loop.
			if key == "items" {
				normalizeForGemini(v)
			}
		}
	}
}

type geminiResponse struct {
	Candidates []struct {
		FinishReason string `json:"finishReason"`
		Content      struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
			Role string `json:"role"`
		} `json:"content"`
	} `json:"candidates"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}
