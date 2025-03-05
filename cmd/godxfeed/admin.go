package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/urfave/cli/v2"
)

func new_bearer_token(ctx *cli.Context) error {
	r, err := http.NewRequest("POST", fmt.Sprintf("%s/token", ctx.String("godxfeed-endpoint")), nil)
	if err != nil {
		return err
	}
	r.SetBasicAuth(ctx.String("username"), ctx.String("password"))

	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	// Parse the JSON response
	type TokenResponse struct {
		Token string `json:"token"`
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return fmt.Errorf("failed to parse token response: %w", err)
	}

	// Get env file path from context
	envPath := ctx.String("env-path")
	if envPath == "" {
		fmt.Printf("%s\n", body)
		return nil
	}

	// Read existing .env file
	content, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read .env file: %w", err)
	}

	// Update or append AUTH_TOKEN using parsed token
	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, "AUTH_TOKEN=") {
			lines[i] = fmt.Sprintf("AUTH_TOKEN=%s", tokenResp.Token)
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("AUTH_TOKEN=%s", tokenResp.Token))
	}

	// Write back to .env file
	err = os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
	if err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}
	fmt.Printf("Auth token written to %s\n", envPath)
	return nil
}

func new_session_token(ctx *cli.Context) error {
	tts, err := setupService(
		ctx.Context,
		getDefaultLogger(ctx.Int("log-level")),
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("streamer-token"),
		true,
		ctx.String("database"),
		ctx.String("nats-url"),
		ctx.String("nats-auth-user"),
		ctx.String("nats-auth-password"),
		ctx.String("nats-godxfeed-user"),
		ctx.String("nats-godxfeed-password"),
		ctx.String("nats-nkey-seed"),
	)
	if err != nil {
		return err
	}

	// Get the session token
	resp, err := tts.NewSessionToken(ctx.String("username"), ctx.String("password"))
	if err != nil {
		return err
	}

	// Parse the JSON response
	type User struct {
		Email       string `json:"email"`
		ExternalID  string `json:"external-id"`
		IsConfirmed bool   `json:"is-confirmed"`
		Username    string `json:"username"`
	}

	type SessionResponse struct {
		User struct {
			User
		} `json:"user"`
		SessionExpiration string `json:"session-expiration"`
		SessionToken      string `json:"session-token"`
	}

	var sessionResp SessionResponse
	if err := json.Unmarshal([]byte(resp.Data), &sessionResp); err != nil {
		return fmt.Errorf("failed to parse session response: %w", err)
	}

	// Use sessionResp.SessionToken instead of raw resp
	envPath := ctx.String("env-path")
	if envPath == "" {
		return writeCLIResponse(resp, nil)
	}

	// Read existing .env file
	content, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read .env file: %w", err)
	}

	// Update the SESSION_TOKEN with parsed token
	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, "SESSION_TOKEN=") {
			lines[i] = fmt.Sprintf("SESSION_TOKEN=%s", sessionResp.SessionToken)
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("SESSION_TOKEN=%s", sessionResp.SessionToken))
	}

	// Write back to .env file
	err = os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
	if err != nil {
		return fmt.Errorf("failed to write .env file: %w", err)
	}
	fmt.Printf("Session token written to %s\n", envPath)
	return nil
}

func dxlink_api_token(ctx *cli.Context) error {
	// Get the session token from env file if env-path is provided
	sessionToken := ctx.String("session-token")
	if envPath := ctx.String("env-path"); envPath != "" {
		content, err := os.ReadFile(envPath)
		if err != nil {
			return fmt.Errorf("failed to read .env file: %w", err)
		}

		lines := strings.Split(string(content), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "SESSION_TOKEN=") {
				sessionToken = strings.TrimPrefix(line, "SESSION_TOKEN=")
				break
			}
		}
	}

	tts, err := setupService(
		ctx.Context,
		getDefaultLogger(ctx.Int("log-level")),
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		sessionToken,
		ctx.String("dxfeed-endpoint"),
		ctx.String("streamer-token"),
		true,
		ctx.String("database"),
		ctx.String("nats-url"),
		ctx.String("nats-auth-user"),
		ctx.String("nats-auth-password"),
		ctx.String("nats-godxfeed-user"),
		ctx.String("nats-godxfeed-password"),
		ctx.String("nats-nkey-seed"),
	)
	if err != nil {
		return err
	}
	resp, err := tts.NewStreamerToken()
	if err != nil {
		return err
	}

	// Get env file path from context
	envPath := ctx.String("env-path")
	if envPath == "" {
		return writeCLIResponse(resp, err)
	}

	// Read existing .env file
	content, err := os.ReadFile(envPath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read .env file: %w", err)
	}

	// Parse the JSON response
	type StreamerResponse struct {
		DxlinkURL string `json:"dxlink-url"`
		ExpiresAt string `json:"expires-at"`
		IssuedAt  string `json:"issued-at"`
		Level     string `json:"level"`
		Token     string `json:"token"`
	}

	var streamerResp StreamerResponse
	if err := json.Unmarshal([]byte(resp.Data), &streamerResp); err != nil {
		return fmt.Errorf("failed to parse streamer response: %w", err)
	}

	// Update or append STREAMER_TOKEN using parsed token
	lines := strings.Split(string(content), "\n")
	found := false
	for i, line := range lines {
		if strings.HasPrefix(line, "STREAMER_TOKEN=") {
			lines[i] = fmt.Sprintf("STREAMER_TOKEN=%s", streamerResp.Token)
			found = true
			break
		}
	}
	if !found {
		lines = append(lines, fmt.Sprintf("STREAMER_TOKEN=%s", streamerResp.Token))
	}

	// Write back to .env file
	err = os.WriteFile(envPath, []byte(strings.Join(lines, "\n")), 0644)
	if err != nil {
		return fmt.Errorf("failed to write env file: %w", err)
	}
	fmt.Printf("STREAMER_TOKEN written to %s\n", envPath)
	return nil
}
