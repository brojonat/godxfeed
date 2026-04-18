package main

import (
	"github.com/brojonat/godxfeed/service"
	"github.com/urfave/cli/v2"
)

func oauthCfgFromCtx(ctx *cli.Context) service.OAuthConfig {
	return service.OAuthConfig{
		TokenURL:     ctx.String("tw-oauth-token-url"),
		ClientSecret: ctx.String("tw-oauth-client-secret"),
		RefreshToken: ctx.String("tw-oauth-refresh-token"),
	}
}

func setupServiceForDataCmd(ctx *cli.Context) (service.Service, error) {
	return setupService(
		ctx.Context,
		getDefaultLogger(ctx.Int("log-level")),
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("dxfeed-endpoint"),
		oauthCfgFromCtx(ctx),
		true, // minimal-setup: these commands don't need NATS
		ctx.String("nats-url"),
		ctx.String("nats-auth-user"),
		ctx.String("nats-auth-password"),
		ctx.String("nats-godxfeed-user"),
		ctx.String("nats-godxfeed-password"),
		ctx.String("nats-nkey-seed"),
	)
}

func get_symbol_data(ctx *cli.Context) error {
	if err := requireFlags(ctx,
		"tastyworks-endpoint",
		"tw-oauth-token-url",
		"tw-oauth-client-secret",
		"tw-oauth-refresh-token",
		"symbol",
	); err != nil {
		return err
	}
	tts, err := setupServiceForDataCmd(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(
		tts.GetSymbolData(ctx.String("symbol"), ctx.String("symbol-type")))
}

func get_option_chain(ctx *cli.Context) error {
	if err := requireFlags(ctx,
		"tastyworks-endpoint",
		"tw-oauth-token-url",
		"tw-oauth-client-secret",
		"tw-oauth-refresh-token",
		"symbol",
	); err != nil {
		return err
	}
	tts, err := setupServiceForDataCmd(ctx)
	if err != nil {
		return err
	}
	return writeCLIResponse(tts.GetOptionChain(ctx.String("symbol")))
}
