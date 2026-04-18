package analytics

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/nats-io/nats.go"
)

// stubSink is a no-op Sink used to verify the registry wires factories and
// DSNs through correctly without any real backend.
type stubSink struct{ name, dsn string }

func (s *stubSink) Name() string                  { return s.name }
func (s *stubSink) Start(context.Context) error   { return nil }

func stubFactory(name string) Factory {
	return func(_ context.Context, dsn string, _ *nats.Conn, _ *slog.Logger) (Sink, error) {
		return &stubSink{name: name, dsn: dsn}, nil
	}
}

// Every test runs against a saved snapshot of the registry so that mutations
// don't leak across tests (or clobber init-time registrations from sibling
// files like timescale.go).
func withCleanRegistry(t *testing.T, body func(t *testing.T)) {
	t.Helper()
	saved := snapshotRegistryForTest()
	restoreRegistryForTest(map[string]Factory{})
	t.Cleanup(func() { restoreRegistryForTest(saved) })
	body(t)
}

func TestBuild_DispatchesOnScheme(t *testing.T) {
	withCleanRegistry(t, func(t *testing.T) {
		RegisterSink("stub", stubFactory("stub"))
		s, err := Build(context.Background(), "stub://foo/bar?x=1", nil, slog.Default())
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		ss, ok := s.(*stubSink)
		if !ok {
			t.Fatalf("got %T, want *stubSink", s)
		}
		if ss.dsn != "stub://foo/bar?x=1" {
			t.Errorf("dsn = %q, want it passed through unchanged", ss.dsn)
		}
	})
}

func TestBuild_UnknownSchemeErrorLists_KnownOnes(t *testing.T) {
	withCleanRegistry(t, func(t *testing.T) {
		RegisterSink("stub", stubFactory("stub"))
		RegisterSink("other", stubFactory("other"))
		_, err := Build(context.Background(), "mystery://x", nil, slog.Default())
		if err == nil {
			t.Fatal("expected error for unknown scheme")
		}
		// Error should list the schemes that *are* registered — useful for operators.
		if !strings.Contains(err.Error(), "stub") || !strings.Contains(err.Error(), "other") {
			t.Errorf("error should list known schemes; got: %v", err)
		}
	})
}

func TestBuild_RejectsDSNWithoutScheme(t *testing.T) {
	withCleanRegistry(t, func(t *testing.T) {
		_, err := Build(context.Background(), "just-a-path", nil, slog.Default())
		if err == nil {
			t.Fatal("expected error for schemeless DSN")
		}
	})
}

func TestBuild_RejectsMalformedDSN(t *testing.T) {
	withCleanRegistry(t, func(t *testing.T) {
		_, err := Build(context.Background(), "://::::bad", nil, slog.Default())
		if err == nil {
			t.Fatal("expected error for malformed DSN")
		}
	})
}

func TestRegisterSink_PanicsOnDuplicate(t *testing.T) {
	withCleanRegistry(t, func(t *testing.T) {
		RegisterSink("stub", stubFactory("stub"))
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic on duplicate registration")
			}
		}()
		RegisterSink("stub", stubFactory("other"))
	})
}

func TestRegisteredSchemes_SortedAndCopied(t *testing.T) {
	withCleanRegistry(t, func(t *testing.T) {
		RegisterSink("zeta", stubFactory("zeta"))
		RegisterSink("alpha", stubFactory("alpha"))
		RegisterSink("mu", stubFactory("mu"))
		got := RegisteredSchemes()
		want := []string{"alpha", "mu", "zeta"}
		if len(got) != len(want) {
			t.Fatalf("len mismatch: got %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
			}
		}
		// Mutating the returned slice must not affect subsequent calls.
		got[0] = "mutated"
		got2 := RegisteredSchemes()
		if got2[0] == "mutated" {
			t.Error("RegisteredSchemes returned an aliasing slice; callers can corrupt internal state")
		}
	})
}
