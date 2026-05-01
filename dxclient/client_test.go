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

func TestSend_ReturnsErrorOnDeadConnection(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	c := newDialedClient(t, ctx, fs)

	// Kill the connection by cancelling the client's context.
	cancel()
	c.Wait()

	// Send on the dead client must return an error, not block.
	done := make(chan error, 1)
	go func() {
		done <- c.Send(dxclient.MessageKeepalive{
			MessageBase: dxclient.MessageBase{Type: dxclient.MESSAGE_TYPE_KEEPALIVE},
		})
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Send on dead connection must return error, got nil")
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Send on dead connection blocked instead of returning error")
	}
}

func TestDone_ClosedOnConnectionDeath(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	c := newDialedClient(t, ctx, fs)

	cancel()
	select {
	case <-c.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() not closed within 2s of context cancel")
	}
}

func TestC_ClosedOnConnectionDeath(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	c := newDialedClient(t, ctx, fs)

	feed, _ := c.C()
	cancel()

	// The feed channel should close when the connection dies.
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	for {
		select {
		case _, ok := <-feed:
			if !ok {
				return // success: channel closed
			}
			// Drain any in-flight messages.
		case <-timer.C:
			t.Fatal("C() channel not closed within 2s of connection death")
		}
	}
}
