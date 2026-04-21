package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newAnthropicFake spins up an httptest.NewServer that records the
// inbound request and responds with whatever JSON the handler closes
// over. Returns the provider pointed at the fake, plus the last
// captured request for assertion.
func newAnthropicFake(t *testing.T, respStatus int, respBody string) (*AnthropicProvider, *capturedReq) {
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
	return &AnthropicProvider{
		APIKey:  "test-key",
		Model:   "claude-opus-4-7",
		BaseURL: srv.URL,
		HTTP:    srv.Client(),
		Version: "2023-06-01",
	}, cap
}

type capturedReq struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

// TestAnthropicProvider_RequestShape — the wire format the provider
// sends is the contract with Anthropic. If we break it, every
// subscription request fails in prod; pin every field that matters.
func TestAnthropicProvider_RequestShape(t *testing.T) {
	resp := `{
		"content": [{"type": "tool_use", "name": "extract_filter", "input": {
			"root": "SPY",
			"event_types": ["Quote"],
			"kind": "",
			"expiry_window": null,
			"strike_window": null,
			"include_equity": true
		}}],
		"stop_reason": "tool_use"
	}`
	p, cap := newAnthropicFake(t, http.StatusOK, resp)

	_, err := p.Extract(context.Background(), "subscribe to SPY", mustTime("2026-04-20"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}

	if cap.method != "POST" {
		t.Errorf("method = %s, want POST", cap.method)
	}
	if cap.path != "/v1/messages" {
		t.Errorf("path = %s, want /v1/messages", cap.path)
	}
	if cap.headers.Get("x-api-key") != "test-key" {
		t.Errorf("missing or wrong x-api-key header: %q", cap.headers.Get("x-api-key"))
	}
	if cap.headers.Get("anthropic-version") == "" {
		t.Errorf("missing anthropic-version header")
	}
	if cap.headers.Get("content-type") != "application/json" {
		t.Errorf("content-type = %q, want application/json", cap.headers.Get("content-type"))
	}

	var body map[string]any
	if err := json.Unmarshal(cap.body, &body); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if body["model"] != "claude-opus-4-7" {
		t.Errorf("model echo = %v, want claude-opus-4-7", body["model"])
	}
	// System prompt must include today's date so relative dates resolve.
	if sys, _ := body["system"].(string); !strings.Contains(sys, "2026-04-20") {
		t.Errorf("system prompt missing today's date (2026-04-20): %q", sys)
	}
	// tool_choice must force extract_filter — a naked tool_use request
	// would let the model answer in prose.
	tc, _ := body["tool_choice"].(map[string]any)
	if tc == nil || tc["type"] != "tool" || tc["name"] != "extract_filter" {
		t.Errorf("tool_choice = %v, want forced extract_filter", tc)
	}
}

func TestAnthropicProvider_ParsesToolUseResponse(t *testing.T) {
	resp := `{
		"content": [
			{"type": "text", "text": "ignored prose"},
			{"type": "tool_use", "name": "extract_filter", "input": {
				"root": "SPY",
				"event_types": ["Quote", "Greeks"],
				"kind": "call",
				"expiry_window": {"min": "2026-03-01", "max": "2026-03-31"},
				"strike_window": {"min": 250, "max": 270},
				"include_equity": false
			}}
		],
		"stop_reason": "tool_use"
	}`
	p, _ := newAnthropicFake(t, http.StatusOK, resp)

	got, err := p.Extract(context.Background(), "SPY calls 250-270 march", mustTime("2026-04-20"))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got.Root != "SPY" || got.Kind != "call" {
		t.Errorf("got = %+v", got)
	}
	if got.StrikeWindow == nil || got.StrikeWindow.Min != 250 || got.StrikeWindow.Max != 270 {
		t.Errorf("strike window wrong: %+v", got.StrikeWindow)
	}
	if got.ExpiryWindow == nil || got.ExpiryWindow.Min != "2026-03-01" || got.ExpiryWindow.Max != "2026-03-31" {
		t.Errorf("expiry window wrong: %+v", got.ExpiryWindow)
	}
	if len(got.EventTypes) != 2 || got.EventTypes[0] != "Quote" {
		t.Errorf("event types wrong: %+v", got.EventTypes)
	}
}

func TestAnthropicProvider_MissingToolUseErrors(t *testing.T) {
	// Model refused to use the tool — returned only a text block.
	// This is a real failure mode (content policy, prompt injection
	// attempts). Must error loudly rather than returning an empty
	// FilterSpec that would then resolve to zero subs.
	resp := `{
		"content": [{"type": "text", "text": "I can't help with that."}],
		"stop_reason": "end_turn"
	}`
	p, _ := newAnthropicFake(t, http.StatusOK, resp)

	_, err := p.Extract(context.Background(), "whatever", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error when no tool_use in response")
	}
	if !strings.Contains(err.Error(), "stop_reason") {
		t.Errorf("error should surface stop_reason: %v", err)
	}
}

func TestAnthropicProvider_NonOKStatusErrors(t *testing.T) {
	p, _ := newAnthropicFake(t, http.StatusTooManyRequests, `{"error":"rate limited"}`)

	_, err := p.Extract(context.Background(), "SPY", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error on 429")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Errorf("error should include status code: %v", err)
	}
}

func TestAnthropicProvider_MalformedResponseErrors(t *testing.T) {
	p, _ := newAnthropicFake(t, http.StatusOK, `{not json`)
	_, err := p.Extract(context.Background(), "SPY", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want parse error")
	}
}

func TestAnthropicProvider_MissingAPIKeyErrorsFast(t *testing.T) {
	// Fail before we try to dial anything — a clear error at the
	// entry point beats a cryptic 401 from the upstream.
	p := &AnthropicProvider{Model: "claude-opus-4-7"}
	_, err := p.Extract(context.Background(), "SPY", mustTime("2026-04-20"))
	if err == nil {
		t.Fatal("want error when APIKey unset")
	}
}

func mustTime(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(fmt.Sprintf("mustTime: %v", err))
	}
	return t
}
