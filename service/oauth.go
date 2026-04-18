package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// OAuthConfig holds the Personal OAuth Grant credentials minted in the
// Tastytrade web UI. The refresh token is long-lived; the access token is
// short-lived (~15m) and is obtained on demand from the token endpoint.
type OAuthConfig struct {
	TokenURL     string
	ClientSecret string
	RefreshToken string
}

// TokenProvider hands out a currently-valid Tastytrade OAuth access token,
// refreshing transparently when the cached one is near expiry.
type TokenProvider interface {
	AccessToken(ctx context.Context) (string, error)
}

// clockSkew is subtracted from the server-reported TTL so we refresh slightly
// before the token actually expires. Matches the 30s the official JS SDK uses.
const clockSkew = 30 * time.Second

type tokenProvider struct {
	cfg    OAuthConfig
	http   *http.Client
	now    func() time.Time
	group  singleflight.Group

	mu       sync.RWMutex
	token    string
	expireAt time.Time
}

func newTokenProvider(cfg OAuthConfig, hc *http.Client, now func() time.Time) (*tokenProvider, error) {
	if cfg.TokenURL == "" {
		return nil, fmt.Errorf("oauth: TokenURL is required")
	}
	if cfg.ClientSecret == "" {
		return nil, fmt.Errorf("oauth: ClientSecret is required")
	}
	if cfg.RefreshToken == "" {
		return nil, fmt.Errorf("oauth: RefreshToken is required")
	}
	if hc == nil {
		hc = http.DefaultClient
	}
	if now == nil {
		now = time.Now
	}
	return &tokenProvider{cfg: cfg, http: hc, now: now}, nil
}

// NewTokenProvider constructs a TokenProvider for production use.
func NewTokenProvider(cfg OAuthConfig) (TokenProvider, error) {
	return newTokenProvider(cfg, http.DefaultClient, time.Now)
}

func (p *tokenProvider) AccessToken(ctx context.Context) (string, error) {
	if tok, ok := p.cachedToken(); ok {
		return tok, nil
	}
	v, err, _ := p.group.Do("refresh", func() (interface{}, error) {
		// Re-check under the flight in case another caller just refreshed.
		if tok, ok := p.cachedToken(); ok {
			return tok, nil
		}
		return p.refresh(ctx)
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

func (p *tokenProvider) cachedToken() (string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.token == "" {
		return "", false
	}
	if !p.now().Before(p.expireAt) {
		return "", false
	}
	return p.token, true
}

type refreshRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	ClientSecret string `json:"client_secret"`
}

type refreshResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (p *tokenProvider) refresh(ctx context.Context) (string, error) {
	body, err := json.Marshal(refreshRequest{
		GrantType:    "refresh_token",
		RefreshToken: p.cfg.RefreshToken,
		ClientSecret: p.cfg.ClientSecret,
	})
	if err != nil {
		return "", fmt.Errorf("oauth: marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.TokenURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("oauth: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("oauth: token endpoint: %w", err)
	}
	defer resp.Body.Close()

	rbody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("oauth: read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("oauth: token endpoint returned %d: %s", resp.StatusCode, bytes.TrimSpace(rbody))
	}

	var parsed refreshResponse
	if err := json.Unmarshal(rbody, &parsed); err != nil {
		return "", fmt.Errorf("oauth: parse response: %w", err)
	}
	if parsed.AccessToken == "" {
		return "", fmt.Errorf("oauth: empty access_token in response: %s", rbody)
	}
	ttl := time.Duration(parsed.ExpiresIn) * time.Second
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}

	p.mu.Lock()
	p.token = parsed.AccessToken
	p.expireAt = p.now().Add(ttl - clockSkew)
	p.mu.Unlock()

	return parsed.AccessToken, nil
}
