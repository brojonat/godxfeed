package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/brojonat/godxfeed/service"
	"github.com/urfave/cli/v2"
)

// new_bearer_token requests a godxfeed (not tastytrade) bearer JWT from the
// HTTP server's POST /token endpoint using basic auth, and writes it to the
// supplied env file as AUTH_TOKEN.
func new_bearer_token(ctx *cli.Context) error {
	if err := requireFlags(ctx, "godxfeed-endpoint", "email", "server-secret"); err != nil {
		return err
	}
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/token", ctx.String("godxfeed-endpoint")), nil)
	if err != nil {
		return err
	}
	r.SetBasicAuth(ctx.String("email"), ctx.String("server-secret"))

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var tokenResp struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	envFile := ctx.String("env-file")
	if envFile == "" {
		fmt.Printf("%s\n", body)
		return nil
	}
	if err := upsertEnv(envFile, "AUTH_TOKEN", tokenResp.Token); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "AUTH_TOKEN written to %s\n", envFile)
	return nil
}

// get_streamer_token obtains a dxFeed streamer token from tastytrade using the
// OAuth Personal Grant credentials. Intended as a sanity check — the HTTP
// server fetches its own streamer token on demand inside the stream pipeline.
func get_streamer_token(ctx *cli.Context) error {
	if err := requireFlags(ctx,
		"tastyworks-endpoint",
		"tw-oauth-token-url",
		"tw-oauth-client-secret",
		"tw-oauth-refresh-token",
	); err != nil {
		return err
	}
	tp, err := service.NewTokenProvider(service.OAuthConfig{
		TokenURL:     ctx.String("tw-oauth-token-url"),
		ClientSecret: ctx.String("tw-oauth-client-secret"),
		RefreshToken: ctx.String("tw-oauth-refresh-token"),
	})
	if err != nil {
		return fmt.Errorf("oauth config: %w", err)
	}
	baseURL := "https://" + ctx.String("tastyworks-endpoint")
	td, err := service.FetchStreamerToken(ctx.Context, http.DefaultClient, baseURL, tp)
	if err != nil {
		return err
	}
	b, err := json.Marshal(td)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", b)
	return nil
}

// upsertEnv replaces or appends `KEY=value` in the named env file.
func upsertEnv(path, key, value string) error {
	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read %s: %w", path, err)
	}
	prefix := key + "="
	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, prefix) {
			lines[i] = prefix + value
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, prefix+value)
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0644)
}
