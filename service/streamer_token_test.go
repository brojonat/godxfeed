package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// staticToken is a TokenProvider that always returns the same access token,
// so streamer-token tests can assert on the Authorization header without
// standing up the OAuth endpoint too.
type staticToken string

func (s staticToken) AccessToken(context.Context) (string, error) { return string(s), nil }

// TestFetchStreamerToken_UsesBearer verifies the tastytrade /api-quote-tokens
// request carries Authorization: Bearer <access_token> and parses the response
// into api.TokenData.
func TestFetchStreamerToken_UsesBearer(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{
				"token":      "streamer-abc",
				"dxlink-url": "wss://tasty-openapi-ws.dxfeed.com/realtime",
				"level":      "api",
			},
		})
	}))
	defer srv.Close()

	tp := staticToken("access-42")
	td, err := fetchStreamerToken(context.Background(), srv.Client(), srv.URL, tp)
	if err != nil {
		t.Fatalf("fetchStreamerToken: %v", err)
	}

	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/api-quote-tokens" {
		t.Errorf("path = %s, want /api-quote-tokens", gotPath)
	}
	if gotAuth != "Bearer access-42" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer access-42")
	}
	if td.Token != "streamer-abc" {
		t.Errorf("token = %q", td.Token)
	}
	if td.DXLinkURL != "wss://tasty-openapi-ws.dxfeed.com/realtime" {
		t.Errorf("dxlink-url = %q", td.DXLinkURL)
	}
	if td.Level != "api" {
		t.Errorf("level = %q", td.Level)
	}
}

// TestFetchStreamerToken_PropagatesHTTPError: 4xx/5xx from tastytrade must be
// surfaced as error, not silently returned as an empty TokenData.
func TestFetchStreamerToken_PropagatesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"code":"unauthorized","message":"bad token"}}`))
	}))
	defer srv.Close()

	_, err := fetchStreamerToken(context.Background(), srv.Client(), srv.URL, staticToken("x"))
	if err == nil {
		t.Fatal("expected error on 401")
	}
}

// TestFetchStreamerToken_ProviderError: if the TokenProvider fails, the error
// is surfaced without making any HTTP call.
func TestFetchStreamerToken_ProviderError(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
	}))
	defer srv.Close()

	failing := failingProvider{err: context.DeadlineExceeded}
	_, err := fetchStreamerToken(context.Background(), srv.Client(), srv.URL, failing)
	if err == nil {
		t.Fatal("expected provider error to propagate")
	}
	if hit {
		t.Error("must not hit tastytrade when provider fails")
	}
}

type failingProvider struct{ err error }

func (f failingProvider) AccessToken(context.Context) (string, error) { return "", f.err }

// silence unused-import warnings when building in isolation.
var _ = time.Second
