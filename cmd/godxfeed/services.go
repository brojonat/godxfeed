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
// large, but it's basically an entire implementation of the service. The ctx
// is used to configure the handlers. In the future, we can easily be more
// dynamic with the handler selection here.
func getSymbolDataHandlers(tts service.Service, ctx *cli.Context) []func([]byte) error {
	// if the no-stream flag is set, don't return any handlers
	handlers := []func([]byte) error{}
	if ctx.Bool("no-symbol-handlers") {
		return nil
	}

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

	// publish the data to NATS
	publish := func(b []byte) error {
		err := tts.PublishSymbolData(b)
		if err != nil {
			tts.Log(
				int(slog.LevelError),
				"error publishing data to NATS",
				"error", err.Error(),
			)
		}
		return nil
	}

	// add handlers based on the CLI context
	if ctx.Bool("handler-debug") {
		handlers = append(handlers, debug)
	}
	if ctx.Bool("handler-persist") {
		handlers = append(handlers, persist)
	}
	if ctx.Bool("handler-publish") {
		handlers = append(handlers, publish)
	}
	return handlers
}

// serve_http is the main entry point for the CLI. It sets up the service,
// gets the symbols, optionally runs the streamer (that may write to the DB and/or
// publish to NATS) in a separate goroutine, and then finally runs the HTTP server.
//
// It returns an error if the service setup fails, or if the symbols cannot be
// retrieved.
func serve_http(ctx *cli.Context) error {
	tts, err := setupService(
		ctx.Context,
		getDefaultLogger(ctx.Int("log-level")),
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("streamer-token"),
		false,
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
	if ctx.Bool("symbol-method-n-related") && len(symbols) != 1 {
		return fmt.Errorf("symbol-method-n-related requires exactly one symbol")
	}

	syms := []string{}
	if ctx.Bool("symbol-method-n-related") {
		syms, err = tts.GetStreamSymbols(
			symbols[0],
			service.SymbolMethodNRelatedOptions(tts, ctx.Int("max-symbol-count")),
		)
		if err != nil {
			return fmt.Errorf("could not get symbol data: %v", err)
		}
	} else {
		syms = symbols
	}

	// Get handlers that handle messages. You can configure your handlers
	// depending on the CLI context! For example, you can configure your handlers
	// to record data, to not record data, maybe just record some data, etc.
	handlers := getSymbolDataHandlers(tts, ctx)

	// If we've set symbols and handlers, then run a function that streams the
	// symbols to the handlers we defined above. If you need to handle panic
	// recovery or other stateful errors, you can do so here, but for now that
	// is out of scope. Also note that for the time being, we're only handling
	// StreamAPIFeedCompactQuoteData (since that is what the Service currently
	// makes available); any other message types are ignored.
	if handlers != nil {
		go func() {
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
	)
}
