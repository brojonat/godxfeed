package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// QuotesStreamName is the JetStream stream that captures all quote
// publishes. The Go ingress publishes via plain nats.Publish — JetStream
// captures matching subjects automatically.
const QuotesStreamName = "QUOTES"

// EnsureQuotesStream creates (or updates) the QUOTES JetStream stream.
// The stream captures every subject under godxfeed.quote.> and retains
// messages for maxAge. Memory storage keeps latency low; the stream is
// a replay buffer for analytic sidecars, not durable persistence.
func EnsureQuotesStream(ctx context.Context, nc *nats.Conn, maxAge time.Duration, log *slog.Logger) error {
	js, err := jetstream.New(nc)
	if err != nil {
		return fmt.Errorf("jetstream context: %w", err)
	}

	cfg := jetstream.StreamConfig{
		Name:      QuotesStreamName,
		Subjects:  []string{"godxfeed.quote.>"},
		MaxAge:    maxAge,
		Storage:   jetstream.MemoryStorage,
		Retention: jetstream.LimitsPolicy,
		Discard:   jetstream.DiscardOld,
	}

	_, err = js.CreateOrUpdateStream(ctx, cfg)
	if err != nil {
		return fmt.Errorf("create/update stream %s: %w", QuotesStreamName, err)
	}

	log.Info("JetStream stream ensured",
		"stream", QuotesStreamName,
		"subjects", cfg.Subjects,
		"max_age", maxAge.String(),
		"storage", "memory",
	)
	return nil
}
