package main

import (
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"os"
	"strings"

	dx "github.com/brojonat/godxfeed/dxclient"
	tt "github.com/brojonat/godxfeed/tastytrade"
	"github.com/urfave/cli/v2"
)

func main() {
	tts := tt.NewService()
	app := &cli.App{
		Commands: []*cli.Command{
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
					return new_session_token(ctx, tts)
				},
			},
			{
				Name:  "get-streamer-token",
				Usage: "Get a new DXLink API token.",
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
				},
				Action: func(ctx *cli.Context) error {
					return dxlink_api_token(ctx, tts)
				},
			},
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
						Name:    "symbol",
						Aliases: []string{"s"},
						Usage:   "Underlying symbol or product code (if required by symbol-type).",
					},
				},
				Action: func(ctx *cli.Context) error {
					return get_symbol_data(ctx, tts)
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
					return get_option_chain(ctx, tts)
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
					return stream_symbol(ctx, tts)
				},
			},
			{
				Name:  "serve-http",
				Usage: "Run the HTTP server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "listen-address",
						Aliases: []string{"listen", "la", "a"},
						Value:   ":8080",
						Usage:   "Address to listen on.",
					},
					&cli.StringFlag{
						Name:    "tastyworks-endpoint",
						Aliases: []string{"tw"},
						Value:   "api.tastyworks.com",
						Usage:   "TWAPI base url.",
					},
					&cli.StringFlag{
						Name:  "session-token",
						Value: os.Getenv("SESSION_TOKEN"),
						Usage: "TWAPI session token. Expires every 24h. Use `new-session-token` to get a new token.",
					},
					&cli.StringFlag{
						Name:    "streamer-endpoint",
						Aliases: []string{"se"},
						Value:   "tasty-openapi-ws.dxfeed.com/realtime",
						Usage:   "DXLINK streaming endpoint.",
					},
					&cli.StringFlag{
						Name:  "streamer-token",
						Value: os.Getenv("STREAMER_TOKEN"),
						Usage: "DXLINK auth token.",
					},
				},
				Action: func(ctx *cli.Context) error {
					return serve_http(ctx, tts)
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

func writeCLIResponse(twr *tt.Response, err error) error {
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

func new_session_token(ctx *cli.Context, tts tt.Service) error {
	return writeCLIResponse(
		tts.NewSessionToken(
			ctx.String("tastyworks-endpoint"),
			ctx.String("username"),
			ctx.String("password"),
		))
}

func dxlink_api_token(ctx *cli.Context, tts tt.Service) error {
	return writeCLIResponse(
		tts.NewStreamerToken(
			ctx.String("tastyworks-endpoint"),
			ctx.String("session-token"),
		))
}

func get_symbol_data(ctx *cli.Context, tts tt.Service) error {
	return writeCLIResponse(
		tts.GetSymbolData(
			ctx.String("tastyworks-endpoint"),
			ctx.String("session-token"),
			ctx.String("symbol"),
			ctx.String("symbol-type"),
		))
}

func get_option_chain(ctx *cli.Context, tts tt.Service) error {
	return writeCLIResponse(
		tts.GetOptionChain(
			ctx.String("tastyworks-endpoint"),
			ctx.String("session-token"),
			ctx.String("symbol"),
			ctx.Bool("nested"),
			ctx.Bool("compact"),
		))
}

func serve_http(ctx *cli.Context, tts tt.Service) error {
	return tt.RunHTTPServer(
		tts,
		ctx.String("listen-address"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("streamer-endpoint"),
		ctx.String("streamer-token"),
	)
}

func stream_symbol(ctx *cli.Context, tts tt.Service) error {

	// Get symbols. Only streaming top SYMBOL_COUNT symbols for now; will have to chunk
	// subscription calls in the future.
	syms, err := tts.GetRelatedSymbols(
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("symbol"),
	)
	if err != nil {
		return fmt.Errorf("could not get symbol data: %v", err)
	}
	if ctx.Int("symbol-count") < 1 {
		return fmt.Errorf("symbol-count must be >=1")
	}
	syms = syms[0:ctx.Int("symbol-count")]

	// get streamer credentials
	twr, err := tts.NewStreamerToken(ctx.String("tastyworks-endpoint"), ctx.String("session-token"))
	if err != nil {
		return fmt.Errorf("could not get streamer token: %w", err)
	}
	var dxData tt.TokenData
	if err := json.Unmarshal(twr.Data, &dxData); err != nil {
		return fmt.Errorf("could not unmarshal dx token data: %w", err)
	}

	// stream
	// FIXME: TastyWorks isn't actually serving the options data just yet, so the vanilla
	// symbol will have to do for now
	c, err := tts.StreamSymbols(ctx.Context, dxData.DXLinkURL, dxData.Token, syms)
	if err != nil {
		return fmt.Errorf("could not get stream symbol data: %w", err)
	}
	for b := range c {
		fmt.Printf("%s\n", b)
	}
	return nil
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
