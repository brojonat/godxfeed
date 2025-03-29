package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/brojonat/godxfeed/service/api"
	"github.com/urfave/cli/v2"
)

func getDefaultLogger(lvl int) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		AddSource: true,
		Level:     slog.Level(lvl),
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				source, _ := a.Value.Any().(*slog.Source)
				if source != nil {
					source.Function = ""
					source.File = filepath.Base(source.File)
				}
			}
			return a
		},
	}))
}

func writeCLIResponse(twr *api.Response, err error) error {
	if err != nil {
		return err
	}
	b, err := json.Marshal(twr)
	if err != nil {
		return err
	}
	fmt.Printf("%s\n", b)
	return nil
}

func main() {
	app := &cli.App{
		Commands: []*cli.Command{
			{
				Name:  "debug",
				Usage: "Debugging commands",
				Subcommands: []*cli.Command{
					{
						Name:  "publish-nats",
						Usage: "Publish messages to NATS",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "nats-url",
								Usage:   "NATS URL",
								EnvVars: []string{"NATS_URL"},
							},
							&cli.StringFlag{
								Name:    "nats-godxfeed-user",
								Usage:   "NATS godxfeed user.",
								EnvVars: []string{"NATS_GODXFEED_USER"},
							},
							&cli.StringFlag{
								Name:    "nats-godxfeed-password",
								Usage:   "NATS godxfeed password.",
								EnvVars: []string{"NATS_GODXFEED_PASSWORD"},
							},
							&cli.StringSliceFlag{
								Name:     "nats-topic",
								Usage:    "NATS topics (can be specified multiple times)",
								Required: true,
							},
							&cli.StringFlag{
								Name:  "interval",
								Usage: "Interval to publish messages. Default is 1s.",
								Value: "1s",
							},
						},
						Action: func(ctx *cli.Context) error {
							return publish_nats(ctx)
						},
					},
				},
			},
			{
				Name:  "admin",
				Usage: "Administrative commands",
				Subcommands: []*cli.Command{
					{
						Name:  "get-bearer-token",
						Usage: "Makes an HTTP request for a new bearer token. Expires every 2 weeks.",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "godxfeed-endpoint",
								Aliases: []string{"tw"},
								EnvVars: []string{"GODXFEED_ENDPOINT"},
								Usage:   "godxfeed HTTP endpoint",
							},
							&cli.StringFlag{
								Name:     "username",
								Aliases:  []string{"u"},
								Usage:    "Email to issue the JWT to.",
								EnvVars:  []string{"TW_USERNAME"},
								Required: false,
							},
							&cli.StringFlag{
								Name:     "password",
								Aliases:  []string{"p"},
								Usage:    "Server secret key used for JWTs.",
								EnvVars:  []string{"TW_PASSWORD"},
								Required: false,
							},
							&cli.StringFlag{
								Name:  "env-file",
								Usage: "Path to env file to update with the new bearer token",
							},
						},
						Action: func(ctx *cli.Context) error {
							return new_bearer_token(ctx)
						},
					},
					{
						Name:  "get-session-token",
						Usage: "Get a new session token. Expires every 24 hours. Don't request more often than necessary.",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "tastyworks-endpoint",
								Aliases: []string{"tw"},
								Value:   "api.tastyworks.com",
								Usage:   "TWAPI base url",
							},
							&cli.StringFlag{
								Name:     "username",
								Aliases:  []string{"u"},
								Usage:    "TWAPI sandbox username",
								Required: true,
							},
							&cli.StringFlag{
								Name:     "password",
								Aliases:  []string{"p"},
								Usage:    "TWAPI sandbox password",
								Required: true,
							},
							&cli.StringFlag{
								Name:  "env-file",
								Usage: "Path to .env file to update with the session token",
								Value: "",
							},
						},
						Action: func(ctx *cli.Context) error {
							return new_session_token(ctx)
						},
					},
					{
						Name:  "get-streamer-token",
						Usage: "Get a new DXLink API token.",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "tastyworks-endpoint",
								Aliases: []string{"tw", "t"},
								Value:   "api.tastyworks.com",
								Usage:   "TWAPI base url",
							},
							&cli.StringFlag{
								Name:    "session-token",
								Aliases: []string{"st", "s"},
								Value:   os.Getenv("SESSION_TOKEN"),
								Usage:   "TWAPI session token. Expires every 24h. Use new-session-token to get a new token.",
							},
							&cli.StringFlag{
								Name:  "env-file",
								Usage: "Path to .env file to update with the streamer token",
								Value: "",
							},
						},
						Action: func(ctx *cli.Context) error {
							return dxlink_api_token(ctx)
						},
					},
				},
			},
			{
				Name:  "data",
				Usage: "Data related commands",
				Subcommands: []*cli.Command{
					{
						Name:  "symbols",
						Usage: "get the symbol data",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "tastyworks-endpoint",
								Aliases: []string{"tw"},
								Value:   "api.tastyworks.com",
								Usage:   "TWAPI base url",
							},
							&cli.StringFlag{
								Name:    "session-token",
								Aliases: []string{"st"},
								EnvVars: []string{"SESSION_TOKEN"},
								Usage:   "TWAPI session token. Expires every 24h. Use `new-session-token` to get a new token.",
							},
							&cli.StringFlag{
								Name:    "symbol-type",
								Aliases: []string{"t"},
								Value:   "equities",
								Usage:   "Symbol type (crypto, futures, equities, options, or futures-options).",
							},
							&cli.StringFlag{
								Name:     "symbol",
								Aliases:  []string{"s"},
								Usage:    "Underlying symbol or product code (if required by symbol-type).",
								Required: true,
							},
						},
						Action: func(ctx *cli.Context) error {
							return get_symbol_data(ctx)
						},
					},
					{
						Name:  "option-chain",
						Usage: "get the option for the symbol",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "tastyworks-endpoint",
								Aliases: []string{"tw"},
								Value:   "api.tastyworks.com",
								Usage:   "TWAPI base url",
							},
							&cli.StringFlag{
								Name:    "session-token",
								Aliases: []string{"st"},
								EnvVars: []string{"SESSION_TOKEN"},
								Usage:   "TWAPI session token. Expires every 24h. Use `new-session-token` to get a new token.",
							},
							&cli.StringFlag{
								Name:    "symbol",
								Aliases: []string{"s"},
								Value:   "SPY",
								Usage:   "Equity symbol of the option chain.",
							},
						},
						Action: func(ctx *cli.Context) error {
							return get_option_chain(ctx)
						},
					},
				},
			},
			{
				Name:  "run",
				Usage: "Run services",
				Subcommands: []*cli.Command{
					{
						Name:  "http-server",
						Usage: "Run the HTTP server",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "listen-port",
								Aliases: []string{"port", "p"},
								Usage:   "Port to listen on",
								EnvVars: []string{"SERVER_PORT"},
							},
							&cli.IntFlag{
								Name:    "log-level",
								Aliases: []string{"ll", "l"},
								Usage:   "Logging level for the slog.Logger. Default is 0 (INFO), use -4 for DEBUG",
								Value:   0,
							},
							&cli.BoolFlag{
								Name:  "minimal-setup",
								Usage: "Minimal setup for the HTTP server.",
								Value: false,
							},
							&cli.StringFlag{
								Name:    "database",
								Aliases: []string{"db", "d"},
								Usage:   "Database endpoint",
								EnvVars: []string{"DATABASE_URL"},
							},
							&cli.StringFlag{
								Name:    "tastyworks-endpoint",
								Aliases: []string{"tw"},
								Usage:   "TWAPI base url.",
								Value:   "api.tastyworks.com",
							},
							&cli.StringFlag{
								Name:    "dxfeed-endpoint",
								Aliases: []string{"se"},
								Usage:   "DXLINK streaming endpoint.",
								Value:   "tasty-openapi-ws.dxfeed.com/realtime",
							},
							&cli.StringFlag{
								Name:    "session-token",
								Usage:   "TWAPI session token. Expires every 24h. Use `new-session-token` to get a new token.",
								EnvVars: []string{"SESSION_TOKEN"},
							},
							&cli.StringFlag{
								Name:    "streamer-token",
								Usage:   "DXLINK auth token.",
								EnvVars: []string{"STREAMER_TOKEN"},
							},
							&cli.StringFlag{
								Name:    "nats-url",
								Usage:   "NATS URL.",
								EnvVars: []string{"NATS_URL"},
							},
							&cli.StringFlag{
								Name:    "nats-browser-url",
								Usage:   "NATS browser URL.",
								EnvVars: []string{"NATS_BROWSER_URL"},
							},
							&cli.StringFlag{
								Name:    "nats-nkey-public-key",
								Usage:   "NATS nkey public key.",
								EnvVars: []string{"NATS_NKEY_PUBLIC_KEY"},
							},
							&cli.StringFlag{
								Name:    "nats-nkey-seed",
								Usage:   "NATS nkey seed.",
								EnvVars: []string{"NATS_NKEY_SEED"},
							},
							&cli.StringFlag{
								Name:    "nats-auth-user",
								Usage:   "NATS auth user.",
								EnvVars: []string{"NATS_AUTH_USER"},
							},
							&cli.StringFlag{
								Name:    "nats-auth-password",
								Usage:   "NATS auth password.",
								EnvVars: []string{"NATS_AUTH_PASSWORD"},
							},
							&cli.StringFlag{
								Name:    "nats-godxfeed-user",
								Usage:   "NATS godxfeed user.",
								EnvVars: []string{"NATS_GODXFEED_USER"},
							},
							&cli.StringFlag{
								Name:    "nats-godxfeed-password",
								Usage:   "NATS godxfeed password.",
								EnvVars: []string{"NATS_GODXFEED_PASSWORD"},
							},
							&cli.IntFlag{
								Name:  "max-symbol-count",
								Usage: "Maximum number of symbols to stream. Takes precedence over symbols.",
								Value: 25,
							},
							&cli.StringSliceFlag{
								Name:    "symbols",
								Aliases: []string{"symbol", "sym", "s"},
								Usage:   "Symbols to stream.",
							},
							&cli.StringFlag{
								Name:  "symbol-method",
								Usage: "Method to use to get symbols.",
								Value: "",
							},
							&cli.BoolFlag{
								Name:  "handler-debug",
								Usage: "Enable the debug logging streamer handler.",
								Value: false,
							},
							&cli.BoolFlag{
								Name:  "handler-persist",
								Usage: "Persist the symbol data to the database.",
								Value: false,
							},
							&cli.BoolFlag{
								Name:  "dev-mode",
								Usage: "Run server in development mode (load templates and static files from disk)",
								Value: false,
							},
						},
						Action: func(ctx *cli.Context) error {
							return serve_http(ctx)
						},
					},
				},
			},
		}}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
