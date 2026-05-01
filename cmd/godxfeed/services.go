package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	ghttp "github.com/brojonat/godxfeed/http"
	"github.com/brojonat/godxfeed/service"
	"github.com/brojonat/godxfeed/service/analytics"
	_ "github.com/brojonat/godxfeed/service/analytics" // blank import to trigger timescale registration
	"github.com/brojonat/godxfeed/service/llm"
	"github.com/urfave/cli/v2"
)

// setupService constructs the Service. The service no longer owns a DB
// connection — persistence is delegated to analytics sinks configured via
// --analytic-sink. Only NATS (for pub/sub + auth callout) and OAuth
// (for tastytrade auth) are required in non-minimal mode.
func setupService(
	ctx context.Context,
	l *slog.Logger,
	listenPort string,
	twEndpoint string,
	dxEndpoint string,
	dxForceURL string,
	oauthCfg service.OAuthConfig,
	minimalSetup bool,
	natsURL string,
	natsAuthUser string,
	natsAuthPassword string,
	natsGodxfeedUser string,
	natsGodxfeedPassword string,
	natsNkeySeed string,
) (service.Service, error) {
	// In mock mode (dxForceURL set), skip OAuth entirely — the mock
	// accepts any token, so there's nothing to exchange.
	var tp service.TokenProvider
	if dxForceURL == "" {
		var err error
		tp, err = service.NewTokenProvider(oauthCfg)
		if err != nil {
			return nil, fmt.Errorf("oauth config: %w", err)
		}
	}

	if minimalSetup {
		l.Debug("initializing service with minimal dependencies")
		return service.NewService(twEndpoint, dxEndpoint, dxForceURL, tp, l, nil), nil
	}

	appNC, err := service.SetupNatsWithAuthCallout(
		ctx,
		natsURL,
		natsAuthUser,
		natsAuthPassword,
		natsGodxfeedUser,
		natsGodxfeedPassword,
		natsNkeySeed,
		fmt.Sprintf("http://localhost:%s/nats-auth-callout", listenPort),
	)
	if err != nil {
		return nil, fmt.Errorf("could not setup nats: %w", err)
	}
	return service.NewService(twEndpoint, dxEndpoint, dxForceURL, tp, l, appNC), nil
}

// serve_http is the CLI entry point. It validates env, constructs the service,
// fetches the symbol set to subscribe to, starts every configured analytics
// sink as a NATS subscriber, kicks off the dxLink → NATS ingress goroutine,
// and then serves the HTTP API.
func serve_http(ctx *cli.Context) error {
	required := []string{
		"listen-port",
		"tastyworks-endpoint",
		"dxfeed-endpoint",
	}
	// OAuth is only needed when we're actually dialing tastytrade's
	// dxFeed gateway. A --dxfeed-url override points at a local mock,
	// which accepts any token.
	if ctx.String("dxfeed-url") == "" {
		required = append(required,
			"tw-oauth-token-url",
			"tw-oauth-client-secret",
			"tw-oauth-refresh-token",
		)
	}
	if !ctx.Bool("minimal-setup") {
		required = append(required,
			"nats-url",
			"nats-auth-user",
			"nats-auth-password",
			"nats-godxfeed-user",
			"nats-godxfeed-password",
			"nats-nkey-seed",
		)
	}
	if err := requireFlags(ctx, required...); err != nil {
		return err
	}

	log := getDefaultLogger(ctx.Int("log-level"))

	tts, err := setupService(
		ctx.Context,
		log,
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("dxfeed-url"),
		service.OAuthConfig{
			TokenURL:     ctx.String("tw-oauth-token-url"),
			ClientSecret: ctx.String("tw-oauth-client-secret"),
			RefreshToken: ctx.String("tw-oauth-refresh-token"),
		},
		ctx.Bool("minimal-setup"),
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

	// Resolve the symbols to subscribe to. Empty is valid — it just means
	// "boot the server but don't subscribe to anything yet" (useful when
	// paired with the dummy publisher or a future POST /dxlink/subscriptions).
	syms, err := resolveStreamSymbols(tts, ctx)
	if err != nil {
		return err
	}

	// Start every configured analytic sink. Each one opens its own NATS
	// subscription and backend connection and runs for the lifetime of the
	// server. A sink failure doesn't take down the server — it just logs and
	// exits.
	if !ctx.Bool("minimal-setup") {
		// Ensure the QUOTES JetStream stream exists before sinks or
		// ingress start. The stream captures godxfeed.quote.> with a
		// short retention window so analytic sidecars can replay recent
		// quotes on restart. Non-fatal: if JetStream isn't available
		// (e.g. account not configured), log and continue — plain NATS
		// subscriptions still work.
		if err := service.EnsureQuotesStream(ctx.Context, tts.NATS(), 10*time.Minute, log); err != nil {
			log.Warn("JetStream stream setup failed; analytics sidecar will fall back to plain NATS", "err", err)
		}

		for _, dsn := range ctx.StringSlice("analytic-sink") {
			sink, err := analytics.Build(ctx.Context, dsn, tts.NATS(), log)
			if err != nil {
				return fmt.Errorf("analytic-sink %q: %w", dsn, err)
			}
			go func(s analytics.Sink) {
				log.Info("starting analytic sink", "name", s.Name())
				if err := s.Start(ctx.Context); err != nil {
					log.Error("analytic sink exited with error", "name", s.Name(), "err", err)
				}
			}(sink)
		}

		// Start the dxLink → NATS ingress unconditionally. An empty symbol
		// set is legal — the feed channel is opened anyway so runtime
		// POST /dxlink/subscriptions adds work against a live client.
		if err := tts.StartIngress(ctx.Context, syms); err != nil {
			log.Error("failed to start ingress", "err", err)
		}
	}

	llmRegistry := buildLLMRegistry(ctx, log)

	return ghttp.RunHTTPServer(
		ctx.Context,
		tts,
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("nats-browser-url"),
		ctx.Bool("dev-mode"),
		llmRegistry,
	)
}

// buildLLMRegistry inspects the LLM-related flags/envs and constructs
// one Provider per configured key. Missing keys = that provider isn't
// registered, and /nl-subscribe will 400 if the caller asks for it.
// The default provider is whichever --llm-default names, as long as
// it's actually registered; otherwise the first successfully-built
// provider wins (alphabetical) so there's always *some* usable default
// when at least one key is configured.
func buildLLMRegistry(ctx *cli.Context, log *slog.Logger) ghttp.ProviderRegistry {
	reg := ghttp.ProviderRegistry{Providers: map[string]llm.Provider{}}

	if key := ctx.String("anthropic-api-key"); key != "" {
		reg.Providers["anthropic"] = llm.NewAnthropicProvider(key, orDefault(ctx.String("anthropic-model"), "claude-opus-4-7"))
	}
	if key := ctx.String("openai-api-key"); key != "" {
		reg.Providers["openai"] = llm.NewOpenAIProvider(key, orDefault(ctx.String("openai-model"), "gpt-4o"))
	}
	if key := ctx.String("gemini-api-key"); key != "" {
		reg.Providers["gemini"] = llm.NewGeminiProvider(key, orDefault(ctx.String("gemini-model"), "gemini-2.5-pro"))
	}

	// Default: honor --llm-default if it names a registered provider;
	// otherwise pick deterministically (alphabetical first) so callers
	// that omit the "provider" field on a request still work.
	want := ctx.String("llm-default")
	if _, ok := reg.Providers[want]; ok {
		reg.Default = want
	} else if len(reg.Providers) > 0 {
		// Pick in a stable order so restarts don't silently switch.
		for _, name := range []string{"anthropic", "openai", "gemini"} {
			if _, ok := reg.Providers[name]; ok {
				reg.Default = name
				break
			}
		}
	}

	if len(reg.Providers) == 0 {
		log.Info("no LLM providers configured; /nl-subscribe will 503")
	} else {
		names := make([]string, 0, len(reg.Providers))
		for n := range reg.Providers {
			names = append(names, n)
		}
		log.Info("LLM providers registered", "providers", names, "default", reg.Default)
	}
	return reg
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// resolveStreamSymbols applies the --symbol-method strategy to the raw symbol
// list from the CLI.
func resolveStreamSymbols(tts service.Service, ctx *cli.Context) ([]string, error) {
	symbols := ctx.StringSlice("symbols")
	if ctx.Int("max-symbol-count") < 0 {
		return nil, fmt.Errorf("max-symbol-count must be greater than or equal to 0")
	}
	switch ctx.String("symbol-method") {
	case "n-related":
		if len(symbols) != 1 {
			return nil, fmt.Errorf("symbol method `n-related` requires exactly one symbol")
		}
		syms, err := tts.GetStreamSymbols(
			symbols[0],
			service.SymbolMethodNRelatedOptions(tts, ctx.Int("max-symbol-count")),
		)
		if err != nil {
			return nil, fmt.Errorf("could not get symbol data: %w", err)
		}
		return syms, nil
	default:
		return symbols, nil
	}
}
