package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newGeminiFake(t *testing.T, respStatus int, respBody string) (*GeminiProvider, *capturedReq) {
	t.Helper()
	cap := &capturedReq{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cap.method = r.Method
		cap.path = r.URL.Path
		cap.headers = r.Header.Clone()
		cap.body, _ = io.ReadAll(r.Body)
		w.Header().Set("content-type", "application/json")
		w.WriteHeader(respStatus)
		_, _ = w.Write([]byte(respBody))
	}))
	t.Cleanup(srv.Close)
	return &GeminiProvider{
		APIKey:  "test-key",
		Model:   "gemini-2.5-pro",
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}, cap
}

// geminiContentResp wraps a JSON content string in the envelope the
// generateContent API returns.
func geminiContentResp(filterJSON string) string {
	env := map[string]any{
		"candidates": []map[string]any{{
			"finishReason": "STOP",
			"content": map[string]any{
				"role":  "model",
				"parts": []map[string]any{{"text": filterJSON}},
			},
		}},
	}
	b, _ := json.Marshal(env)
	return string(b)
}

func TestGeminiProvider_RequestShape(t *testing.T) {
	resp := geminiContentResp(`{
		"root": "SPY",
		"event_types": ["Quote"],
		"kind": "",
		"expiry_window": null,
		"strike_window": null,
		"include_equity": true
	}`)
	p, cap := newGeminiFake(t, http.StatusOK, resp)

	_, err := p.Extract(context.Background(), "subscribe to SPY", mustTime("2026-04-20"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if cap.path != "/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Errorf("path = %s", cap.path)
	}
	if cap.headers.Get("x-goog-api-key") != "test-key" {
		t.Errorf("x-goog-api-key header = %q", cap.headers.Get("x-goog-api-key"))
	}

	var body map[string]any
	if err := json.Unmarshal(cap.body, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	gc, _ := body["generationConfig"].(map[string]any)
	if gc == nil || gc["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig.responseMimeType = %v, want application/json", gc)
	}

	// responseSchema must be the Gemini dialect — no `type: [...]`
	// unions, nullable fields marked via `nullable: true`.
	rs, _ := gc["responseSchema"].(map[string]any)
	if rs == nil {
		t.Fatalf("responseSchema missing")
	}
	props, _ := rs["properties"].(map[string]any)
	ew, _ := props["expiry_window"].(map[string]any)
	if ew == nil {
		t.Fatalf("expiry_window missing from schema")
	}
	if _, isSlice := ew["type"].([]any); isSlice {
		t.Errorf("expiry_window.type is still a union — not Gemini-adapted")
	}
	if ty, _ := ew["type"].(string); ty != "object" {
		t.Errorf("expiry_window.type = %v, want scalar 'object'", ew["type"])
	}
	if n, _ := ew["nullable"].(bool); !n {
		t.Errorf("expiry_window.nullable should be true after adaptation")
	}
}

func TestGeminiProvider_ParsesStructuredResponse(t *testing.T) {
	resp := geminiContentResp(`{
		"root": "TSLA",
		"event_types": ["Quote", "TheoPrice"],
		"kind": "call",
		"expiry_window": {"min": "2026-06-01", "max": "2026-06-30"},
		"strike_window": {"min": 200, "max": 250},
		"include_equity": false
	}`)
	p, _ := newGeminiFake(t, http.StatusOK, resp)

	got, err := p.Extract(context.Background(), "TSLA calls 200-250 june, theoprice", mustTime("2026-04-20"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got.Root != "TSLA" || got.Kind != "call" {
		t.Errorf("got = %+v", got)
	}
	if got.StrikeWindow == nil || got.StrikeWindow.Max != 250 {
		t.Errorf("strike window wrong: %+v", got.StrikeWindow)
	}
	gotEvents := strings.Join(got.EventTypes, ",")
	if !strings.Contains(gotEvents, "TheoPrice") {
		t.Errorf("missing TheoPrice in %s", gotEvents)
	}
}

func TestGeminiProvider_BlockedPromptErrors(t *testing.T) {
	// Gemini returns zero candidates + a promptFeedback reason when
	// blocked. Must surface that clearly.
	body := `{"candidates":[],"promptFeedback":{"blockReason":"SAFETY"}}`
	p, _ := newGeminiFake(t, http.StatusOK, body)
	_, err := p.Extract(context.Background(), "whatever", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error on blocked prompt")
	}
	if !strings.Contains(err.Error(), "SAFETY") {
		t.Errorf("error should surface block reason: %v", err)
	}
}

func TestGeminiProvider_NonSTOPFinishReasonErrors(t *testing.T) {
	// A non-STOP finish (e.g. MAX_TOKENS, SAFETY) means the response
	// likely isn't complete/valid JSON. Treat as error.
	env := map[string]any{
		"candidates": []map[string]any{{
			"finishReason": "MAX_TOKENS",
			"content": map[string]any{
				"role":  "model",
				"parts": []map[string]any{{"text": `{"root":"SP`}},
			},
		}},
	}
	b, _ := json.Marshal(env)
	p, _ := newGeminiFake(t, http.StatusOK, string(b))
	_, err := p.Extract(context.Background(), "huge prompt", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error on non-STOP finish")
	}
}

func TestGeminiProvider_NonOKStatusErrors(t *testing.T) {
	p, _ := newGeminiFake(t, http.StatusBadRequest, `{"error":{"code":400,"message":"bad"}}`)
	_, err := p.Extract(context.Background(), "SPY", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error on 400")
	}
}

func TestGeminiSchema_NormalizesNullableUnions(t *testing.T) {
	// Unit test for the adapter directly — both nullable sub-schemas
	// (expiry_window, strike_window) should be translated.
	s, err := geminiSchema()
	if err != nil {
		t.Fatalf("geminiSchema: %v", err)
	}
	props, _ := s["properties"].(map[string]any)
	for _, name := range []string{"expiry_window", "strike_window"} {
		node, _ := props[name].(map[string]any)
		if node == nil {
			t.Fatalf("%s missing", name)
		}
		if _, isSlice := node["type"].([]any); isSlice {
			t.Errorf("%s: type union not rewritten", name)
		}
		if !(node["type"] == "object" && node["nullable"] == true) {
			t.Errorf("%s: type=%v nullable=%v, want object + nullable:true", name, node["type"], node["nullable"])
		}
	}
}
