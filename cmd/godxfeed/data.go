package main

import (
	"github.com/urfave/cli/v2"
)

func get_symbol_data(ctx *cli.Context) error {
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
	return writeCLIResponse(
		tts.GetSymbolData(ctx.String("symbol"), ctx.String("symbol-type")))
}

func get_option_chain(ctx *cli.Context) error {
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
	return writeCLIResponse(tts.GetOptionChain(ctx.String("symbol")))
}
