package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/brojonat/godxfeed/service/api"
)

// FetchStreamerToken calls tastytrade's GET /api-quote-tokens using a Bearer
// access token from the supplied TokenProvider and returns the streamer token
// payload. baseURL is expected to be the full scheme+host (e.g.
// "https://api.tastyworks.com"); the path is appended.
func FetchStreamerToken(ctx context.Context, hc *http.Client, baseURL string, tp TokenProvider) (api.TokenData, error) {
	return fetchStreamerToken(ctx, hc, baseURL, tp)
}

func fetchStreamerToken(ctx context.Context, hc *http.Client, baseURL string, tp TokenProvider) (api.TokenData, error) {
	if hc == nil {
		hc = http.DefaultClient
	}
	access, err := tp.AccessToken(ctx)
	if err != nil {
		return api.TokenData{}, fmt.Errorf("streamer-token: get access token: %w", err)
	}

	url := strings.TrimRight(baseURL, "/") + "/api-quote-tokens"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return api.TokenData{}, fmt.Errorf("streamer-token: build request: %w", err)
	}
	addDefaultHeaders(req.Header)
	req.Header.Set("Authorization", "Bearer "+access)

	resp, err := hc.Do(req)
	if err != nil {
		return api.TokenData{}, fmt.Errorf("streamer-token: do request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.TokenData{}, fmt.Errorf("streamer-token: read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return api.TokenData{}, fmt.Errorf("streamer-token: tastytrade returned %d: %s", resp.StatusCode, body)
	}

	var env struct {
		Data api.TokenData `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return api.TokenData{}, fmt.Errorf("streamer-token: parse body: %w", err)
	}
	if env.Data.Token == "" {
		return api.TokenData{}, fmt.Errorf("streamer-token: empty token in response: %s", body)
	}
	return env.Data, nil
}
