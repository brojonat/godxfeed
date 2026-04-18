package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// helper: build a TokenProvider pointed at the test server.
func newTestProvider(t *testing.T, srv *httptest.Server, now func() time.Time) *tokenProvider {
	t.Helper()
	tp, err := newTokenProvider(OAuthConfig{
		TokenURL:     srv.URL + "/oauth/token",
		ClientSecret: "secret-abc",
		RefreshToken: "refresh-xyz",
	}, srv.Client(), now)
	if err != nil {
		t.Fatalf("newTokenProvider: %v", err)
	}
	return tp
}

// TestTokenProvider_FetchesAccessToken is the happy path: first call POSTs to
// the OAuth endpoint and returns the access_token from the JSON response.
func TestTokenProvider_FetchesAccessToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "access-token-1",
			"token_type":   "Bearer",
			"expires_in":   900,
		})
	}))
	defer srv.Close()

	tp := newTestProvider(t, srv, time.Now)
	tok, err := tp.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "access-token-1" {
		t.Errorf("got %q want %q", tok, "access-token-1")
	}
}

// TestTokenProvider_RequestShape verifies the exact wire format tastytrade-api-js
// uses: POST with Content-Type: application/json, body with grant_type +
// refresh_token + client_secret, and NO Authorization header on the token
// endpoint itself.
func TestTokenProvider_RequestShape(t *testing.T) {
	var gotBody map[string]any
	var gotHeaders http.Header
	var gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeaders = r.Header.Clone()
		b, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(b, &gotBody); err != nil {
			t.Fatalf("bad body JSON: %v; raw=%s", err, b)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "a", "token_type": "Bearer", "expires_in": 900,
		})
	}))
	defer srv.Close()

	tp := newTestProvider(t, srv, time.Now)
	if _, err := tp.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if ct := gotHeaders.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if auth := gotHeaders.Get("Authorization"); auth != "" {
		t.Errorf("Authorization header must be empty on token endpoint, got %q", auth)
	}
	if gotBody["grant_type"] != "refresh_token" {
		t.Errorf("grant_type = %v, want refresh_token", gotBody["grant_type"])
	}
	if gotBody["refresh_token"] != "refresh-xyz" {
		t.Errorf("refresh_token = %v, want refresh-xyz", gotBody["refresh_token"])
	}
	if gotBody["client_secret"] != "secret-abc" {
		t.Errorf("client_secret = %v, want secret-abc", gotBody["client_secret"])
	}
}

// TestTokenProvider_CachesUntilNearExpiry: a second call well inside the TTL
// must reuse the cached token and not hit the network.
func TestTokenProvider_CachesUntilNearExpiry(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "token-" + string(rune('0'+n)),
			"token_type":   "Bearer",
			"expires_in":   900,
		})
	}))
	defer srv.Close()

	now := time.Unix(1_700_000_000, 0)
	clock := now
	tp := newTestProvider(t, srv, func() time.Time { return clock })

	first, err := tp.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("first AccessToken: %v", err)
	}
	// advance 10 minutes — still well inside the 15-minute TTL (minus skew)
	clock = clock.Add(10 * time.Minute)
	second, err := tp.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("second AccessToken: %v", err)
	}
	if first != second {
		t.Errorf("expected cached token; first=%q second=%q", first, second)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1", got)
	}
}

// TestTokenProvider_RefreshesWhenExpired: once the clock passes the expiry
// (accounting for skew), the next call triggers a fresh exchange.
func TestTokenProvider_RefreshesWhenExpired(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		var tok string
		if n == 1 {
			tok = "first"
		} else {
			tok = "second"
		}
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": tok, "token_type": "Bearer", "expires_in": 900,
		})
	}))
	defer srv.Close()

	clock := time.Unix(1_700_000_000, 0)
	tp := newTestProvider(t, srv, func() time.Time { return clock })

	first, err := tp.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	if first != "first" {
		t.Fatalf("first token = %q", first)
	}
	// move well past the TTL
	clock = clock.Add(20 * time.Minute)
	second, err := tp.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if second != "second" {
		t.Errorf("second token = %q, want second", second)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Errorf("server hits = %d, want 2", got)
	}
}

// TestTokenProvider_Returns401AsError: the OAuth server returns 401; the
// provider must surface a non-nil error and not cache the bad token.
func TestTokenProvider_Returns401AsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"invalid_grant"}`)
	}))
	defer srv.Close()

	tp := newTestProvider(t, srv, time.Now)
	_, err := tp.AccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error on 401; got nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") && !strings.Contains(err.Error(), "401") {
		t.Errorf("error should mention the server reason, got: %v", err)
	}
}

// TestTokenProvider_ServerError500: 5xx should also propagate as error.
func TestTokenProvider_ServerError500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	tp := newTestProvider(t, srv, time.Now)
	_, err := tp.AccessToken(context.Background())
	if err == nil {
		t.Fatal("expected error on 500; got nil")
	}
}

// TestTokenProvider_ConcurrentCallsShareRefresh: N goroutines call AccessToken
// simultaneously with no cached token; exactly one HTTP request should be made.
func TestTokenProvider_ConcurrentCallsShareRefresh(t *testing.T) {
	var hits int32
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-release // force all in-flight callers to pile up before we respond
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "shared-token", "token_type": "Bearer", "expires_in": 900,
		})
	}))
	defer srv.Close()

	tp := newTestProvider(t, srv, time.Now)

	const n = 8
	var wg sync.WaitGroup
	tokens := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens[i], errs[i] = tp.AccessToken(context.Background())
		}(i)
	}
	// give goroutines a moment to start and issue their request
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("caller %d: %v", i, err)
		}
		if tokens[i] != "shared-token" {
			t.Errorf("caller %d: token = %q", i, tokens[i])
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("server hits = %d, want 1 (singleflight dedup)", got)
	}
}

// TestTokenProvider_ContextCancellation: a canceled context propagates.
func TestTokenProvider_ContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// block until client cancels
		<-r.Context().Done()
	}))
	defer srv.Close()

	tp := newTestProvider(t, srv, time.Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := tp.AccessToken(ctx)
	if err == nil {
		t.Fatal("expected error on canceled ctx")
	}
	if !errors.Is(err, context.Canceled) && !strings.Contains(err.Error(), "canceled") {
		t.Errorf("error should mention cancellation, got: %v", err)
	}
}
