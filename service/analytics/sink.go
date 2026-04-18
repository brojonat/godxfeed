// Package analytics defines the Sink interface — a pluggable consumer of the
// NATS `godxfeed.>` firehose that persists events to some backend. The core
// service knows nothing about concrete sinks; each implementation registers
// itself under a DSN scheme at init-time and the CLI wires them up by passing
// one or more DSNs on --analytic-sink.
package analytics

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"sort"
	"sync"

	"github.com/nats-io/nats.go"
)

// Sink consumes raw NATS messages from godxfeed.> and persists them. Each
// Sink owns its own NATS subscription and backend connection. Start blocks
// until ctx is canceled; implementations should return cleanly on ctx done.
type Sink interface {
	Name() string
	Start(ctx context.Context) error
}

// Factory constructs a Sink from a DSN. The NATS connection is supplied by
// the caller so every sink shares the service's connection pool.
type Factory func(ctx context.Context, dsn string, nc *nats.Conn, log *slog.Logger) (Sink, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// RegisterSink associates a URL scheme with a Factory. Called from init() in
// each implementation file (timescale.go, etc.). Panics on duplicate scheme
// registration — that's a programmer error, not a runtime one.
func RegisterSink(scheme string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[scheme]; exists {
		panic(fmt.Sprintf("analytics: scheme %q is already registered", scheme))
	}
	registry[scheme] = f
}

// Build parses a DSN, dispatches on scheme, and returns a ready-to-Start Sink.
func Build(ctx context.Context, dsn string, nc *nats.Conn, log *slog.Logger) (Sink, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("analytics: parse DSN %q: %w", dsn, err)
	}
	scheme := u.Scheme
	if scheme == "" {
		return nil, fmt.Errorf("analytics: DSN %q has no scheme (expected e.g. postgres://...)", dsn)
	}
	registryMu.RLock()
	f, ok := registry[scheme]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("analytics: no sink registered for scheme %q (known: %v)", scheme, RegisteredSchemes())
	}
	return f(ctx, dsn, nc, log)
}

// RegisteredSchemes returns the currently-registered schemes, sorted.
func RegisteredSchemes() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// snapshotRegistryForTest returns a copy of the current registry; used by
// tests to save state before mutation. Paired with restoreRegistryForTest.
func snapshotRegistryForTest() map[string]Factory {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]Factory, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

func restoreRegistryForTest(snap map[string]Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry = make(map[string]Factory, len(snap))
	for k, v := range snap {
		registry[k] = v
	}
}
