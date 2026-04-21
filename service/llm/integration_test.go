package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// Integration tests — hit real LLM APIs when keys are configured in
// the environment. Skipped otherwise so `go test ./...` stays offline.
//
// Usage:
//
//	set -a && . service/.env.dev && set +a
//	go test ./service/llm/ -run TestIntegration -v
//
// Each test exercises one provider's full Extract() path against the
// live API: builds the schema, makes the POST, parses the structured
// response. A failing test means either the key is bad, the model
// name is wrong, or the provider changed something on us.

// assertFilterShape is the shared "did the LLM do its job" check.
// Every provider should pull SPY as the root, calls as the kind, and
// a March 2026 expiry window from the prompt.
func assertFilterShape(t *testing.T, f FilterSpec, providerName string) {
	t.Helper()
	if f.Root != "SPY" {
		t.Errorf("%s: root = %q, want SPY", providerName, f.Root)
	}
	if f.Kind != "call" {
		t.Errorf("%s: kind = %q, want call", providerName, f.Kind)
	}
	if f.StrikeWindow == nil {
		t.Errorf("%s: strike_window missing", providerName)
	} else if f.StrikeWindow.Min != 250 || f.StrikeWindow.Max != 270 {
		t.Errorf("%s: strike_window = %+v, want {250, 270}", providerName, f.StrikeWindow)
	}
	if f.ExpiryWindow == nil {
		t.Errorf("%s: expiry_window missing", providerName)
	} else {
		// We said "in March" with today = 2026-04-20, so the model
		// should either pick March 2026 (past, historical interpretation)
		// or March 2027 (future interpretation). Either is a reasonable
		// extraction — we just want a March window somewhere.
		if !strings.Contains(f.ExpiryWindow.Min, "-03-") {
			t.Errorf("%s: expiry min = %q, want a March date", providerName, f.ExpiryWindow.Min)
		}
		if !strings.Contains(f.ExpiryWindow.Max, "-03-") {
			t.Errorf("%s: expiry max = %q, want a March date", providerName, f.ExpiryWindow.Max)
		}
	}
}

const smokeTestPrompt = "subscribe me to SPY calls between 250 and 270 expiring in March"

// smokeTestTime is fixed so "in March" always has the same set of
// valid interpretations (this year's past March, or next year's).
var smokeTestTime = time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)

func TestIntegration_Anthropic(t *testing.T) {
	key := os.Getenv("ANTHROPIC_API_KEY")
	if key == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}
	model := os.Getenv("ANTHROPIC_MODEL")
	if model == "" {
		model = "claude-opus-4-7"
	}
	p := NewAnthropicProvider(key, model)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	t0 := time.Now()
	got, err := p.Extract(ctx, smokeTestPrompt, smokeTestTime)
	if err != nil {
		t.Fatalf("anthropic extract (model=%s): %v", model, err)
	}
	t.Logf("anthropic/%s: %s — got %+v", model, time.Since(t0).Round(time.Millisecond), got)
	assertFilterShape(t, got, "anthropic")
}

func TestIntegration_OpenAI(t *testing.T) {
	key := os.Getenv("OPENAI_API_KEY")
	if key == "" {
		t.Skip("OPENAI_API_KEY not set")
	}
	model := os.Getenv("OPENAI_MODEL")
	if model == "" {
		model = "gpt-4o"
	}
	p := NewOpenAIProvider(key, model)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	t0 := time.Now()
	got, err := p.Extract(ctx, smokeTestPrompt, smokeTestTime)
	if err != nil {
		t.Fatalf("openai extract (model=%s): %v", model, err)
	}
	t.Logf("openai/%s: %s — got %+v", model, time.Since(t0).Round(time.Millisecond), got)
	assertFilterShape(t, got, "openai")
}

func TestIntegration_Gemini(t *testing.T) {
	key := os.Getenv("GEMINI_API_KEY")
	if key == "" {
		t.Skip("GEMINI_API_KEY not set")
	}
	model := os.Getenv("GEMINI_MODEL")
	if model == "" {
		model = "gemini-2.5-pro"
	}
	p := NewGeminiProvider(key, model)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	t0 := time.Now()
	got, err := p.Extract(ctx, smokeTestPrompt, smokeTestTime)
	if err != nil {
		t.Fatalf("gemini extract (model=%s): %v", model, err)
	}
	t.Logf("gemini/%s: %s — got %+v", model, time.Since(t0).Round(time.Millisecond), got)
	assertFilterShape(t, got, "gemini")
}

// TestIntegration_AllProvidersAgree is a softer sanity check: if all
// three keys are set, confirm they extract compatible intents from the
// same prompt. A divergence here usually means one model is being fed
// the prompt in a structurally different way.
func TestIntegration_AllProvidersAgree(t *testing.T) {
	if os.Getenv("ANTHROPIC_API_KEY") == "" || os.Getenv("OPENAI_API_KEY") == "" || os.Getenv("GEMINI_API_KEY") == "" {
		t.Skip("need all three provider keys for cross-provider sanity check")
	}
	providers := []Provider{
		NewAnthropicProvider(os.Getenv("ANTHROPIC_API_KEY"), envOr("ANTHROPIC_MODEL", "claude-opus-4-7")),
		NewOpenAIProvider(os.Getenv("OPENAI_API_KEY"), envOr("OPENAI_MODEL", "gpt-4o")),
		NewGeminiProvider(os.Getenv("GEMINI_API_KEY"), envOr("GEMINI_MODEL", "gemini-2.5-pro")),
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	for _, p := range providers {
		got, err := p.Extract(ctx, smokeTestPrompt, smokeTestTime)
		if err != nil {
			t.Errorf("%s: %v", p.Name(), err)
			continue
		}
		assertFilterShape(t, got, p.Name())
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// Build tag only on the Print helper so the package still compiles
// without unused-import warnings when these tests are skipped.
var _ = fmt.Sprint
