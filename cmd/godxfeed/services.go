package main

import (
	"context"
	"fmt"

	"github.com/brojonat/godxfeed/http"
	"github.com/brojonat/godxfeed/service"
	"github.com/brojonat/godxfeed/service/db/dbgen"
	"github.com/brojonat/server-tools/stools"
	"github.com/jackc/pgx/v5"
	"github.com/urfave/cli/v2"
	"go.temporal.io/sdk/client"
)

func setupService(ctx *cli.Context) (service.Service, error) {
	// logger
	l := getDefaultLogger(ctx.Int("log-level"))

	// minimal setup lets callers get a service instance without the "heavy"
	// dependencies (i.e., db, temporal client), which is useful if all they
	// need to do is get a token from the tastyworks api.
	minimalSetup, ok := ctx.Context.Value(ctxKeyMinimalSetup).(bool)
	if ok && minimalSetup {
		l.Debug("initializing service with minimal dependencies")
		tts := service.NewService(
			ctx.String("tastyworks-endpoint"),
			ctx.String("session-token"),
			ctx.String("dxfeed-endpoint"),
			ctx.String("streamer-token"),
			l, nil, nil, nil)
		return tts, nil
	}

	// temporal
	if ctx.String("temporal-host") == "" {
		return nil, fmt.Errorf("must set temporal connection flag")
	}
	tc, err := client.Dial(client.Options{
		Logger:   l,
		HostPort: ctx.String("temporal-host"),
	})
	if err != nil {
		return nil, fmt.Errorf("could not initialize Temporal client: %w", err)
	}

	// db
	if ctx.String("database") == "" {
		return nil, fmt.Errorf("must set database connection flag")
	}
	p, err := stools.GetConnPool(
		ctx.Context, ctx.String("database"),
		func(ctx context.Context, c *pgx.Conn) error { return nil },
	)
	if err != nil {
		return nil, fmt.Errorf("could not connect to db: %w", err)
	}
	q := dbgen.New(p)

	tts := service.NewService(
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("streamer-token"),
		l, tc, p, q,
	)
	return tts, nil
}

func serve_http(ctx *cli.Context) error {
	tts, err := setupService(ctx)
	if err != nil {
		return err
	}
	return http.RunHTTPServer(
		ctx.Context,
		tts,
		ctx.String("listen-port"),
		ctx.String("tastyworks-endpoint"),
		ctx.String("session-token"),
		ctx.String("dxfeed-endpoint"),
		ctx.String("streamer-token"),
	)
}

// func run_worker(ctx *cli.Context) error {
// 	logger := getDefaultLogger(slog.Level(ctx.Int("log-level")))
// 	thp := ctx.String("temporal-host")
// 	return worker.RunWorker(ctx.Context, logger, thp)
// }
