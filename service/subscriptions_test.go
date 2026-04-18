package service

import (
	"sync"
	"testing"
	"time"
)

func TestSubscriptionManager_RegisterAndSnapshot(t *testing.T) {
	m := NewSubscriptionManager()
	m.Register("Quote", "SPY", "godxfeed.SPY")
	m.Register("Quote", "AAPL", "godxfeed.AAPL")

	got := m.Snapshot()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Sorted: AAPL before SPY (same event, lexicographic symbol).
	if got[0].Symbol != "AAPL" || got[1].Symbol != "SPY" {
		t.Errorf("unexpected sort order: %+v", got)
	}
	for _, info := range got {
		if info.MsgCount != 0 {
			t.Errorf("fresh registration must have zero msgCount; got %d for %s", info.MsgCount, info.Symbol)
		}
		if !info.FirstSeenAt.IsZero() || !info.LastSeenAt.IsZero() {
			t.Errorf("seen-at must be zero before first observation; got %+v", info)
		}
	}
}

func TestSubscriptionManager_ObserveIncrementsAndTimestamps(t *testing.T) {
	m := NewSubscriptionManager()
	clock := time.Unix(1_700_000_000, 0)
	m.now = func() time.Time { return clock }

	m.Register("Quote", "SPY", "godxfeed.SPY")

	m.Observe("Quote", "SPY")
	clock = clock.Add(5 * time.Second)
	m.Observe("Quote", "SPY")
	clock = clock.Add(5 * time.Second)
	m.Observe("Quote", "SPY")

	snap := m.Snapshot()
	if len(snap) != 1 {
		t.Fatalf("len = %d", len(snap))
	}
	info := snap[0]
	if info.MsgCount != 3 {
		t.Errorf("msgCount = %d, want 3", info.MsgCount)
	}
	// FirstSeenAt must be frozen at the *first* observation, not the most recent.
	if got := info.FirstSeenAt.Unix(); got != 1_700_000_000 {
		t.Errorf("firstSeenAt unix = %d, want 1700000000", got)
	}
	if got := info.LastSeenAt.Unix(); got != 1_700_000_010 {
		t.Errorf("lastSeenAt unix = %d, want 1700000010", got)
	}
}

func TestSubscriptionManager_ObserveUnregisteredIsNoOp(t *testing.T) {
	m := NewSubscriptionManager()
	// No Register called first. Observe should silently do nothing.
	m.Observe("Quote", "SPY")
	snap := m.Snapshot()
	if len(snap) != 0 {
		t.Errorf("Observe must not auto-register; got %+v", snap)
	}
}

func TestSubscriptionManager_RegisterIsIdempotent(t *testing.T) {
	m := NewSubscriptionManager()
	m.Register("Quote", "SPY", "godxfeed.SPY")
	m.Observe("Quote", "SPY")
	m.Observe("Quote", "SPY")
	// Re-registering must not zero the counter — otherwise a "refresh" call
	// would silently wipe traffic stats.
	m.Register("Quote", "SPY", "godxfeed.SPY")
	snap := m.Snapshot()
	if snap[0].MsgCount != 2 {
		t.Errorf("re-Register clobbered counter; got msgCount=%d, want 2", snap[0].MsgCount)
	}
}

func TestSubscriptionManager_Unregister(t *testing.T) {
	m := NewSubscriptionManager()
	m.Register("Quote", "SPY", "godxfeed.SPY")
	m.Register("Quote", "AAPL", "godxfeed.AAPL")
	m.Unregister("Quote", "AAPL")
	snap := m.Snapshot()
	if len(snap) != 1 || snap[0].Symbol != "SPY" {
		t.Errorf("after Unregister got %+v, want only SPY", snap)
	}
	// Unregistering a missing key is a no-op.
	m.Unregister("Quote", "nonexistent")
}

// TestSubscriptionManager_ConcurrentObserveIsSafe hammers Observe from many
// goroutines and asserts the final count matches the number of calls. Proves
// the atomic counters work under contention.
func TestSubscriptionManager_ConcurrentObserveIsSafe(t *testing.T) {
	m := NewSubscriptionManager()
	m.Register("Quote", "SPY", "godxfeed.SPY")

	const (
		goroutines = 16
		perG       = 1000
	)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perG; j++ {
				m.Observe("Quote", "SPY")
			}
		}()
	}
	wg.Wait()

	snap := m.Snapshot()
	want := int64(goroutines * perG)
	if snap[0].MsgCount != want {
		t.Errorf("msgCount = %d, want %d", snap[0].MsgCount, want)
	}
}

func TestSubscriptionManager_SortOrder(t *testing.T) {
	m := NewSubscriptionManager()
	m.Register("Quote", "ZZZ", "godxfeed.ZZZ")
	m.Register("Greeks", "AAA", "godxfeed.AAA")
	m.Register("Quote", "AAA", "godxfeed.AAA")
	m.Register("Greeks", "ZZZ", "godxfeed.ZZZ")

	snap := m.Snapshot()
	want := [][2]string{
		{"Greeks", "AAA"}, {"Greeks", "ZZZ"},
		{"Quote", "AAA"}, {"Quote", "ZZZ"},
	}
	for i, w := range want {
		if snap[i].Event != w[0] || snap[i].Symbol != w[1] {
			t.Errorf("index %d: got (%s,%s), want (%s,%s)", i, snap[i].Event, snap[i].Symbol, w[0], w[1])
		}
	}
}
