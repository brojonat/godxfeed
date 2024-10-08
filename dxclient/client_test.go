package dxclient_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/brojonat/godxfeed/dxclient"
	"github.com/brojonat/godxfeed/mock_server"
	"github.com/matryer/is"
)

func runMockServer(ctx context.Context, addr string) error {
	return mock_server.ListenAndServe(ctx, nil, addr)
}

func TestClientSetup(t *testing.T) {
	is := is.New(t)
	mockServerAddr := ":8080"
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)

	// setup the test server
	testServerDone := make(chan error)
	go func() {
		testServerDone <- runMockServer(ctx, mockServerAddr)
	}()

	// setup the client
	logfunc := func(lvl int, msg string, args ...any) {
		switch lvl {
		case int(slog.LevelDebug):
			slog.Debug(msg, args...)
		case int(slog.LevelInfo):
			slog.Info(msg, args...)
		case int(slog.LevelWarn):
			slog.Warn(msg, args...)
		case int(slog.LevelError):
			slog.Error(msg, args...)
		}
	}
	c := dxclient.NewClient(logfunc)

	// dial the client; this will also send the keepalive and setup messages
	is.NoErr(c.Dial(ctx, "ws://localhost:8080/ws",
		func(ms dxclient.MessageSetup) error { return nil }))

	// returns an error on auth failure, otherwise nil
	is.True(c.Authenticate("bad token value") != nil)
	is.NoErr(c.Authenticate(mock_server.ValidAuthToken))

	// teardown
	cancel()
	c.Wait()
	// <-testServerDone
}
