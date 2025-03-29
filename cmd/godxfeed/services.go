package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	ghttp "github.com/brojonat/godxfeed/http"
	"github.com/brojonat/godxfeed/service"
	"github.com/brojonat/godxfeed/service/db/dbgen"
	"github.com/brojonat/server-tools/stools"
	"github.com/jackc/pgx/v5"
	"github.com/urfave/cli/v2"
)

// setupService creates a service instance.
//
// l: the logger to use
// twEndpoint: the tastyworks endpoint to use
// sessionToken: the session token to use
// dxEndpoint: the dxfeed endpoint to use
// streamerToken: the streamer token to use
// minimalSetup: if true, the service will be initialized with minimal dependencies
func setupService(
	ctx context.Context,
	l *slog.Logger,
	listenPort string,
	twEndpoint string,
	sessionToken string,
	dxEndpoint string,
	streamerToken string,
	minimalSetup bool,
	dbConn string,
	natsURL string,
	natsAuthUser string,
	natsAuthPassword string,
	natsGodxfeedUser string,
	natsGodxfeedPassword string,
	natsNkeySeed string,
	devMode bool,
) (service.Service, error) {

	if minimalSetup {
		l.Debug("initializing service with minimal dependencies")
		tts := service.NewService(
			twEndpoint,
			sessionToken,
			dxEndpoint,
			streamerToken,
			l, nil, nil, nil, nil)
		return tts, nil
	}

	// temporal
	// if ctx.String("temporal-host") == "" {
	// 	return nil, fmt.Errorf("must set temporal connection flag")
	// }
	// tc, err := client.Dial(client.Options{
	// 	Logger:   l,
	// 	HostPort: ctx.String("temporal-host"),
	// })
	// if err != nil {
	// 	return nil, fmt.Errorf("could not initialize Temporal client: %w", err)
	// }

	// db
	if dbConn == "" {
		return nil, fmt.Errorf("must set database connection flag")
	}
	p, err := stools.GetConnPool(
		ctx, dbConn,
		func(ctx context.Context, c *pgx.Conn) error { return nil },
	)
	if err != nil {
		return nil, fmt.Errorf("could not connect to db: %w", err)
	}
	q := dbgen.New(p)

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
	tts := service.NewService(
		twEndpoint,
		sessionToken,
		dxEndpoint,
		streamerToken,
		l, nil, p, q, appNC,
	)
	return tts, nil
}

// getSymbolDataHandlers returns a list of handlers that handle feed
// data for a symbol. You can configure your handlers to record data, to not
// record data, maybe just record some data, etc. This function may grow....very
// large, but it's basically the entire implementation of the dxfeed handlers. Note
// that the CLI ctx is used to configure the handlers. In the future, we can easily
// be more dynamic with the handler selection here.
//
// Note that all handlers are OPT IN ONLY. Here are the supported flags:
//
// - handler-debug: if true, the debug handler will be added
// - handler-persist: if true, the persist handler will be added
func getSymbolDataHandlers(tts service.Service, ctx *cli.Context) []func([]byte) error {

	// log all the data at a debug level
	debug := func(b []byte) error {
		tts.Log(int(slog.LevelDebug), "got symbol data", "data", fmt.Sprintf("%s\n", b))
		return nil
	}

	// persist the data to the database
	persist := func(b []byte) error {
		// write the data to the database
		count, err := tts.RecordSymbolData(b)
		if err != nil {
			tts.Log(
				int(slog.LevelError),
				"error inserting feed data into db",
				"error", err.Error(),
				"count", count,
			)
		}
		return nil
	}

	// add handlers based on the CLI context
	handlers := []func([]byte) error{}
	if ctx.Bool("handler-debug") {
		handlers = append(handlers, debug)
	}
	if ctx.Bool("handler-persist") {
		handlers = append(handlers, persist)
	}
	return handlers
}

// serve_http is the main entry point for the CLI. It sets up the service,
// gets the symbols, optionally runs the streamer (that may write to the DB and/or
// publish to NATS) in a separate goroutine, and then finally runs the HTTP server.
//
// It returns an error if the service setup fails, or if the symbols cannot be
// retrieved. It IS VALID to call this function with no symbols, in which case
// the HTTP server will run but no data will be streamed. By default, the HTTP
// server will run the services:
//
// - HTTP server
// - Info logger
// - NATS publisher (NATS connection is configured via the NATS_* flags)
// - Database writer (database connection is configured via the database flag)
//
// You can run a minimal setup by setting the minimal-setup flag to true. This
// will only run the HTTP server and not the other services except for the info
// logger.
func serve_http(ctx *cli.Context) error {
	tts, err := setupService(
		ctx.Context,
		getDefaultLogger(ctx.Int("log-level")),
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("streamer-token"),
		ctx.Bool("minimal-setup"),
		ctx.String("database"),
		ctx.String("nats-url"),
		ctx.String("nats-auth-user"),
		ctx.String("nats-auth-password"),
		ctx.String("nats-godxfeed-user"),
		ctx.String("nats-godxfeed-password"),
		ctx.String("nats-nkey-seed"),
		ctx.Bool("dev-mode"),
	)
	if err != nil {
		return err
	}

	// Get symbols. Only streaming top SYMBOL_COUNT symbols for now; will have
	// to chunk subscription calls in the future since they apparently limit the
	// number you can subscribe to at once. See the documentation for SymbolMethodRelatedN
	// for more information; you can follow that implementation as an example of how
	// to implement your own symbol fetching method(s). One potential improvement
	// would be to implement a method that extracts the related symbols from the
	// CLI ctx so that callers may specify exact (preconfigured) sets of symbols.
	symbols := ctx.StringSlice("symbols")
	maxSymbolCount := ctx.Int("max-symbol-count")
	if maxSymbolCount < 0 {
		return fmt.Errorf("max-symbol-count must be greater than 0")
	}

	syms := []string{}
	symbolMethod := ctx.String("symbol-method")
	switch symbolMethod {
	case "n-related":
		if len(symbols) != 1 {
			return fmt.Errorf("symbol method`n-related` requires exactly one symbol")
		}
		syms, err = tts.GetStreamSymbols(
			symbols[0],
			service.SymbolMethodNRelatedOptions(tts, ctx.Int("max-symbol-count")),
		)
		if err != nil {
			return fmt.Errorf("could not get symbol data: %v", err)
		}
	default:
		syms = symbols
	}

	// Get handlers that handle messages. You can configure your handlers
	// depending on the CLI context! For example, you can configure your handlers
	// to record data, to not record data, maybe just record some data, etc. See
	// getSymbolDataHandlers for supported handler flags. Note that all handlers
	// are OPT IN ONLY.
	handlers := getSymbolDataHandlers(tts, ctx)

	// if we're not in minimal setup mode, and we have handlers, then run a
	// function that streams the symbols to the handlers we defined above.
	// We can abstract this out into a function if we want to, but for now
	// this is the only place we let anyone connect to the TastyWorks Streamer.
	//
	// If you need to handle panic recovery or other stateful errors, you can
	// do so here, but for now that is out of scope. Also note that for the
	// time being, we're only handling StreamAPIFeedCompactQuoteData (since
	// that is what the Service currently makes available); any other message
	// types are ignored.
	if !ctx.Bool("minimal-setup") {
		if len(handlers) > 0 {
			go func() {
				tts.Log(int(slog.LevelInfo), "setting up streamer", "symbols", syms)
				c, err := tts.StreamAPIFeedCompactQuoteData(ctx.Context, syms)
				if err != nil {
					tts.Log(int(slog.LevelError), fmt.Sprintf("error setting up streamer: %s", err.Error()))
					return
				}
				// for each feed update, call each handler
				for quotes := range c {
					// first serialize to bytes, since that's what the handler's API
					// expects
					b, err := json.Marshal(quotes)
					if err != nil {
						tts.Log(int(slog.LevelError), fmt.Sprintf("error marshalling quotes: %s", err.Error()))
						continue
					}
					// now pass the bytes to each handler
					for _, h := range handlers {
						if err := h(b); err != nil {
							tts.Log(int(slog.LevelError), fmt.Sprintf("error handling symbol: %s", err.Error()))
						}
					}
				}
			}()
		}
	}

	// start the symbol streams
	for _, sym := range syms {
		err := tts.StartSymbolStream(sym)
		if err != nil {
			tts.Log(int(slog.LevelError), fmt.Sprintf("error starting symbol stream: %s", err.Error()))
		}
	}

	// finally run the HTTP server
	return ghttp.RunHTTPServer(
		ctx.Context,
		tts,
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("streamer-token"),
		ctx.String("nats-browser-url"),
		ctx.Bool("dev-mode"),
	)
}
