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

func newOpenAIFake(t *testing.T, respStatus int, respBody string) (*OpenAIProvider, *capturedReq) {
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
	return &OpenAIProvider{
		APIKey:  "test-key",
		Model:   "gpt-4o",
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
	}, cap
}

// openAIContentResp wraps a FilterSpec JSON string in the envelope the
// Chat Completions API returns. The inner content is always a JSON
// STRING (not an object) under structured outputs, matching the wire
// format.
func openAIContentResp(filterJSON string) string {
	msg := map[string]any{
		"choices": []map[string]any{{
			"finish_reason": "stop",
			"message": map[string]any{
				"role":    "assistant",
				"content": filterJSON,
				"refusal": "",
			},
		}},
	}
	b, _ := json.Marshal(msg)
	return string(b)
}

func TestOpenAIProvider_RequestShape(t *testing.T) {
	resp := openAIContentResp(`{
		"root": "SPY",
		"event_types": ["Quote"],
		"kind": "",
		"expiry_window": null,
		"strike_window": null,
		"include_equity": true
	}`)
	p, cap := newOpenAIFake(t, http.StatusOK, resp)

	_, err := p.Extract(context.Background(), "subscribe to SPY", mustTime("2026-04-20"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if cap.path != "/v1/chat/completions" {
		t.Errorf("path = %s", cap.path)
	}
	if cap.headers.Get("authorization") != "Bearer test-key" {
		t.Errorf("authorization header = %q", cap.headers.Get("authorization"))
	}

	var body map[string]any
	if err := json.Unmarshal(cap.body, &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["model"] != "gpt-4o" {
		t.Errorf("model echo = %v", body["model"])
	}
	// response_format must be strict json_schema — anything else lets
	// the model return prose and we'd be back to text parsing.
	rf, _ := body["response_format"].(map[string]any)
	if rf == nil || rf["type"] != "json_schema" {
		t.Fatalf("response_format = %v, want json_schema", rf)
	}
	js, _ := rf["json_schema"].(map[string]any)
	if js == nil || js["strict"] != true {
		t.Errorf("json_schema.strict = %v, want true", js)
	}
	// Messages must carry both system and user roles.
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2", len(msgs))
	}
	sys, _ := msgs[0].(map[string]any)
	if sys["role"] != "system" {
		t.Errorf("first role = %v, want system", sys["role"])
	}
	if s, _ := sys["content"].(string); !strings.Contains(s, "2026-04-20") {
		t.Errorf("system content missing today: %q", s)
	}
}

func TestOpenAIProvider_ParsesStructuredResponse(t *testing.T) {
	resp := openAIContentResp(`{
		"root": "AAPL",
		"event_types": ["Quote", "Greeks"],
		"kind": "put",
		"expiry_window": {"min": "2026-05-01", "max": "2026-05-31"},
		"strike_window": {"min": 150, "max": 170},
		"include_equity": true
	}`)
	p, _ := newOpenAIFake(t, http.StatusOK, resp)

	got, err := p.Extract(context.Background(), "AAPL puts 150-170 may, include stock", mustTime("2026-04-20"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got.Root != "AAPL" || got.Kind != "put" || !got.IncludeEquity {
		t.Errorf("got = %+v", got)
	}
	if got.ExpiryWindow == nil || got.ExpiryWindow.Max != "2026-05-31" {
		t.Errorf("expiry window wrong: %+v", got.ExpiryWindow)
	}
}

func TestOpenAIProvider_RefusalErrors(t *testing.T) {
	// OpenAI surfaces policy refusals via a non-empty `refusal` field
	// on the message. Must error — an empty-spec fallback would
	// resolve to zero subs and look like silent data loss.
	refusal := map[string]any{
		"choices": []map[string]any{{
			"finish_reason": "stop",
			"message": map[string]any{
				"role":    "assistant",
				"content": "",
				"refusal": "I can't help with that request.",
			},
		}},
	}
	b, _ := json.Marshal(refusal)
	p, _ := newOpenAIFake(t, http.StatusOK, string(b))

	_, err := p.Extract(context.Background(), "something sketchy", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error on refusal")
	}
	if !strings.Contains(err.Error(), "refused") {
		t.Errorf("error should mention refusal: %v", err)
	}
}

func TestOpenAIProvider_NonOKStatusErrors(t *testing.T) {
	p, _ := newOpenAIFake(t, http.StatusTooManyRequests, `{"error":{"message":"rate limited"}}`)
	_, err := p.Extract(context.Background(), "SPY", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should include status: %v", err)
	}
}

func TestOpenAIProvider_EmptyChoicesErrors(t *testing.T) {
	p, _ := newOpenAIFake(t, http.StatusOK, `{"choices":[]}`)
	_, err := p.Extract(context.Background(), "SPY", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error on zero choices")
	}
}

func TestOpenAIProvider_MalformedContentErrors(t *testing.T) {
	// content is malformed JSON — parseFilterJSON must surface the error
	// rather than returning a zero-valued FilterSpec.
	resp := openAIContentResp(`not even close to JSON`)
	p, _ := newOpenAIFake(t, http.StatusOK, resp)
	_, err := p.Extract(context.Background(), "SPY", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want parse error on malformed content")
	}
}
