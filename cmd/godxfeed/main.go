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

// oauthFlags returns the flag set used by every command that needs to talk to
// tastytrade on the user's behalf via the Personal OAuth Grant.
func oauthFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{
			Name:    "tastyworks-endpoint",
			Aliases: []string{"tw"},
			EnvVars: []string{"TW_API_HOST"},
			Value:   "api.tastyworks.com",
			Usage:   "tastytrade API base host (sandbox: api.cert.tastyworks.com)",
		},
		&cli.StringFlag{
			Name:    "tw-oauth-token-url",
			EnvVars: []string{"TW_OAUTH_TOKEN_URL"},
			Value:   "https://api.tastyworks.com/oauth/token",
			Usage:   "tastytrade OAuth token endpoint",
		},
		&cli.StringFlag{
			Name:    "tw-oauth-client-secret",
			EnvVars: []string{"TW_OAUTH_CLIENT_SECRET"},
			Usage:   "tastytrade OAuth client secret (from Personal Grant)",
		},
		&cli.StringFlag{
			Name:    "tw-oauth-refresh-token",
			EnvVars: []string{"TW_OAUTH_REFRESH_TOKEN"},
			Usage:   "tastytrade OAuth refresh token (from Personal Grant)",
		},
	}
}

func natsFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "nats-url", EnvVars: []string{"NATS_URL"}},
		&cli.StringFlag{Name: "nats-browser-url", EnvVars: []string{"NATS_BROWSER_URL"}},
		&cli.StringFlag{Name: "nats-nkey-public-key", EnvVars: []string{"NATS_NKEY_PUBLIC_KEY"}},
		&cli.StringFlag{Name: "nats-nkey-seed", EnvVars: []string{"NATS_NKEY_SEED"}},
		&cli.StringFlag{Name: "nats-auth-user", EnvVars: []string{"NATS_AUTH_USER"}},
		&cli.StringFlag{Name: "nats-auth-password", EnvVars: []string{"NATS_AUTH_PASSWORD"}},
		&cli.StringFlag{Name: "nats-godxfeed-user", EnvVars: []string{"NATS_GODXFEED_USER"}},
		&cli.StringFlag{Name: "nats-godxfeed-password", EnvVars: []string{"NATS_GODXFEED_PASSWORD"}},
	}
}

func main() {
	app := &cli.App{
		Commands: []*cli.Command{
			{
				Name:  "admin",
				Usage: "Administrative commands",
				Subcommands: []*cli.Command{
					{
						Name:  "get-bearer-token",
						Usage: "Fetch a godxfeed JWT for web UI auth (not a tastytrade token).",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "godxfeed-endpoint",
								EnvVars: []string{"GODXFEED_ENDPOINT"},
								Usage:   "godxfeed HTTP endpoint (e.g. http://localhost:8080)",
							},
							&cli.StringFlag{
								Name:    "email",
								Aliases: []string{"e"},
								EnvVars: []string{"GODXFEED_ADMIN_EMAIL"},
								Usage:   "Email to embed in the issued JWT.",
							},
							&cli.StringFlag{
								Name:    "server-secret",
								EnvVars: []string{"SERVER_SECRET_KEY"},
								Usage:   "The server's JWT signing secret (same value the server reads from SERVER_SECRET_KEY).",
							},
							&cli.StringFlag{
								Name:  "env-file",
								Usage: "Path to env file to update with the new bearer token",
							},
						},
						Action: new_bearer_token,
					},
					{
						Name:   "get-streamer-token",
						Usage:  "Fetch a dxFeed streamer token from tastytrade via OAuth.",
						Flags:  oauthFlags(),
						Action: get_streamer_token,
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
						Flags: append(oauthFlags(),
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
						),
						Action: get_symbol_data,
					},
					{
						Name:  "option-chain",
						Usage: "get the option chain for the symbol",
						Flags: append(oauthFlags(),
							&cli.StringFlag{
								Name:    "symbol",
								Aliases: []string{"s"},
								Value:   "SPY",
								Usage:   "Equity symbol of the option chain.",
							},
						),
						Action: get_option_chain,
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
						Flags: append(append(oauthFlags(), natsFlags()...),
							&cli.StringFlag{
								Name:    "listen-port",
								Aliases: []string{"port", "p"},
								EnvVars: []string{"SERVER_PORT"},
							},
							&cli.IntFlag{
								Name:    "log-level",
								Aliases: []string{"ll", "l"},
								Usage:   "slog level (0 INFO, -4 DEBUG)",
							},
							&cli.BoolFlag{
								Name:  "minimal-setup",
								Usage: "Run without NATS/dxfeed — HTTP server only.",
							},
							&cli.StringFlag{
								Name:    "dxfeed-endpoint",
								Aliases: []string{"se"},
								Value:   "tasty-openapi-ws.dxfeed.com/realtime",
								Usage:   "DXLINK streaming endpoint (fallback if tastytrade doesn't return a dxlink-url).",
							},
							&cli.StringFlag{
								Name:    "dxfeed-url",
								EnvVars: []string{"DXFEED_URL"},
								Usage:   "Mock-mode override: full WS URL (e.g. ws://localhost:9999/realtime). When set, bypasses tastytrade OAuth and the /api-quote-tokens fetch; the ingress dials this URL directly and authenticates with a dummy token.",
							},
							&cli.IntFlag{
								Name:  "max-symbol-count",
								Usage: "Maximum number of symbols to stream.",
								Value: 25,
							},
							&cli.StringSliceFlag{
								Name:    "symbols",
								Aliases: []string{"symbol", "sym", "s"},
								Usage:   "Symbols to stream.",
							},
							&cli.StringFlag{
								Name:  "symbol-method",
								Usage: "Method to use to get symbols (e.g. n-related).",
							},
							&cli.StringSliceFlag{
								Name:    "analytic-sink",
								EnvVars: []string{"ANALYTIC_SINKS"},
								Usage:   "Analytics sink DSN, repeatable (e.g. postgres://... for TimescaleDB).",
							},
							&cli.BoolFlag{
								Name:  "dev-mode",
								Usage: "Load templates and static files from disk rather than the binary.",
							},
						),
						Action: serve_http,
					},
				},
			},
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
