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
// dialect. Gemini accepts an OpenAPI-ish subset with three important
// differences from strict JSON Schema:
//
//  1. No `type: [...]` unions — nullable fields use `nullable: true`.
//  2. No `additionalProperties` key (at any depth). Gemini rejects
//     the whole request with HTTP 400 if one appears.
//  3. No empty-string members in `enum` arrays — Gemini treats `""`
//     as "cannot be empty" and 400s. The shared schema uses `""` on
//     the `kind` field to mean "equity-only"; we strip the empty
//     value from enum lists for Gemini but keep the type constraint
//     as string, so the model can still return "" at runtime (just
//     won't be enum-validated).
//
// The function walks the parsed schema recursively, rewriting (1) and
// stripping (2) wherever they show up. Anything else passes through
// untouched.
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
	// Gemini rejects additionalProperties at any depth — strip it.
	delete(node, "additionalProperties")

	// Strip empty strings from enum arrays — Gemini rejects them.
	if enum, ok := node["enum"].([]any); ok {
		filtered := enum[:0]
		for _, v := range enum {
			if s, isStr := v.(string); isStr && s == "" {
				continue
			}
			filtered = append(filtered, v)
		}
		if len(filtered) == 0 {
			// An all-empty enum would leave an invalid empty list —
			// drop the enum constraint entirely and let the type
			// keyword do the validation.
			delete(node, "enum")
		} else {
			node["enum"] = filtered
		}
	}

	// Recurse into every child that's itself a schema node.
	//   - `properties` is a map of {name → schema}
	//   - `items` can be a single schema (for typed arrays)
	// We walk everything defensively rather than whitelisting keywords
	// so future schema additions don't silently skip adaptation.
	for _, v := range node {
		switch c := v.(type) {
		case map[string]any:
			normalizeForGemini(c)
		case []any:
			for _, item := range c {
				if m, ok := item.(map[string]any); ok {
					normalizeForGemini(m)
				}
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
