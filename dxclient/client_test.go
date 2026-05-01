package dxclient_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/brojonat/godxfeed/dxclient"
)

func TestClientSetup(t *testing.T) {
	fs := newFakeServer(t)
	fs.validToken = "good-token"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c := dxclient.NewClient(func(lvl int, msg string, args ...any) {
		slog.Default().Log(ctx, slog.Level(lvl), msg, args...)
	})

	if err := c.Dial(ctx, fs.wsURL(), func(dxclient.MessageSetup) error { return nil }); err != nil {
		t.Fatalf("dial: %v", err)
	}

	// bad token must return an error
	if err := c.Authenticate("bad-token"); err == nil {
		t.Fatal("Authenticate with bad token must error, got nil")
	}

	// good token must succeed
	if err := c.Authenticate("good-token"); err != nil {
		t.Fatalf("Authenticate with good token: %v", err)
	}
}
