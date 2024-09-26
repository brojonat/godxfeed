package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	dx "github.com/brojonat/godxfeed/dxclient"
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
				Name:  "admin",
				Usage: "Administrative commands",
				Subcommands: []*cli.Command{
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
								Value:   os.Getenv("SESSION_TOKEN"),
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
								Value:   os.Getenv("SESSION_TOKEN"),
								Usage:   "TWAPI session token. Expires every 24h. Use `new-session-token` to get a new token.",
							},
							&cli.StringFlag{
								Name:    "symbol",
								Aliases: []string{"s"},
								Value:   "SPY",
								Usage:   "Equity symbol of the option chain.",
							},
							&cli.BoolFlag{
								Name:    "compact",
								Aliases: []string{"c"},
								Value:   false,
								Usage:   "Use `compact` format.",
							},
							&cli.BoolFlag{
								Name:    "nested",
								Aliases: []string{"n"},
								Value:   false,
								Usage:   "Use `nested` format.",
							},
						},
						Action: func(ctx *cli.Context) error {
							return get_option_chain(ctx)
						},
					},
					{
						Name:  "stream-symbol",
						Usage: "stream the equity symbol and related option symbols",
						Flags: []cli.Flag{
							&cli.StringFlag{
								Name:    "tastyworks-endpoint",
								Aliases: []string{"tw"},
								Value:   "api.tastyworks.com",
								Usage:   "TWAPI base url",
							},
							&cli.StringFlag{
								Name:  "session-token",
								Value: os.Getenv("SESSION_TOKEN"),
								Usage: "TWAPI session token. Expires every 24h. Use `new-session-token` to get a new token.",
							},
							&cli.StringFlag{
								Name:    "symbol",
								Aliases: []string{"s"},
								Value:   "SPY",
								Usage:   "Equity symbol of the option chain.",
							},
							&cli.IntFlag{
								Name:    "symbol-count",
								Aliases: []string{"count", "c"},
								Value:   1,
								Usage:   "Number of symbols to stream (equity plus options)",
							},
						},
						Action: func(ctx *cli.Context) error {
							return stream_symbol(ctx)
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
								Value:   os.Getenv("SERVER_PORT"),
							},
							&cli.IntFlag{
								Name:    "log-level",
								Aliases: []string{"ll", "l"},
								Usage:   "Logging level for the slog.Logger. Default is 0 (INFO), use -4 for DEBUG",
								Value:   0,
							},
							&cli.StringFlag{
								Name:    "database",
								Aliases: []string{"db", "d"},
								Usage:   "Database endpoint",
								Value:   os.Getenv("DATABASE_URL"),
							},
							&cli.StringFlag{
								Name:    "temporal-host",
								Aliases: []string{"th", "t"},
								Usage:   "Temporal endpoint",
								Value:   os.Getenv("TEMPORAL_HOST"),
							},
							&cli.StringFlag{
								Name:    "tastyworks-endpoint",
								Aliases: []string{"tw"},
								Usage:   "TWAPI base url.",
								Value:   "api.tastyworks.com",
							},
							&cli.StringFlag{
								Name:    "streamer-endpoint",
								Aliases: []string{"se"},
								Usage:   "DXLINK streaming endpoint.",
								Value:   "tasty-openapi-ws.dxfeed.com/realtime",
							},
							&cli.StringFlag{
								Name:  "session-token",
								Usage: "TWAPI session token. Expires every 24h. Use `new-session-token` to get a new token.",
								Value: os.Getenv("SESSION_TOKEN"),
							},
							&cli.StringFlag{
								Name:  "streamer-token",
								Usage: "DXLINK auth token.",
								Value: os.Getenv("STREAMER_TOKEN"),
							},
						},
						Action: func(ctx *cli.Context) error {
							return serve_http(ctx)
						},
					},
				},
			},
			{
				Name:  "debug-dxlink",
				Usage: "Debugging route for DXLink stuff.",
				Flags: []cli.Flag{
					&cli.IntFlag{
						Name:  "log-level",
						Value: 0,
						Usage: "Truncate logs below this level (uses slog levels, -4 for debug and above).",
					},
					&cli.StringFlag{
						Name:  "streamer-endpoint",
						Value: "tasty-openapi-ws.dxfeed.com/realtime",
						Usage: "DXLINK WS endpoint.",
					},
					&cli.StringFlag{
						Name:  "streamer-token",
						Value: os.Getenv("STREAMER_TOKEN"),
						Usage: "DXLINK auth token.",
					},
					&cli.StringFlag{
						Name:    "symbol",
						Aliases: []string{"s"},
						Value:   "SPY",
						Usage:   "Symbol to pull data for.",
					},
				},
				Action: func(ctx *cli.Context) error {
					return debug_dxlink(ctx)
				},
			},
		}}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func debug_dxlink(ctx *cli.Context) error {
	sym := strings.ToUpper(ctx.String("symbol"))
	url := fmt.Sprintf("wss://%s", ctx.String("streamer-endpoint"))
	token := ctx.String("streamer-token")

	lvl := new(slog.LevelVar)
	if level := ctx.Int("log-level"); level != 0 {
		lvl.Set(slog.Level(level))
	}

	c := dx.NewClient()
	if err := c.Dial(ctx.Context, url, func(ms dx.MessageSetup) error { return nil }); err != nil {
		return fmt.Errorf("dial failed: %w", err)
	}
	if err := c.Authenticate(token); err != nil {
		return fmt.Errorf("auth failed: %w", err)
	}
	if err := c.Subscribe([]string{sym}); err != nil {
		return fmt.Errorf("could not subscribe to %s: %v", sym, err)
	}

	done := make(chan error)
	hid := "readloop"
	c.AddMessageHandler(hid, func(m dx.Message) {
		msg, ok := m.(dx.MessageFeedData)
		if !ok {
			return
		}

		b, err := msg.JSON()
		if err != nil {
			fmt.Printf("error: %v\n", err)
			return
		}

		fmt.Printf("%s\n", string(b))
	})
	<-done

	return nil
}
