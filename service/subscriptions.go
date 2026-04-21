package service

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brojonat/godxfeed/dxclient"
)

// SubscriptionInfo is the public snapshot shape returned by
// SubscriptionManager.Snapshot and emitted by GET /dxlink/subscriptions.
type SubscriptionInfo struct {
	Event       string    `json:"event"`
	Symbol      string    `json:"symbol"`
	Subject     string    `json:"subject"`
	MsgCount    int64     `json:"msgCount"`
	FirstSeenAt time.Time `json:"firstSeenAt,omitzero"`
	LastSeenAt  time.Time `json:"lastSeenAt,omitzero"`
}

// DXLinkStatus describes the health of the service's one dxLink WebSocket.
type DXLinkStatus struct {
	Connected     bool   `json:"connected"`
	Authenticated bool   `json:"authenticated"`
	DXLinkURL     string `json:"dxlinkURL"`
}

// FeedController is the slice of dxclient.Client that SubscriptionManager
// needs to drive add/remove on the wire. Kept as a local interface (rather
// than accepting dxclient.Client directly) so tests can inject a fake
// without satisfying the full Client surface.
type FeedController interface {
	UpdateSubscription(add, remove []dxclient.FeedSub) error
}

type subKey struct {
	event  string
	symbol string
}

type subState struct {
	subject     string
	msgCount    atomic.Int64
	firstSeenAt atomic.Int64 // unix nanos, 0 = unset
	lastSeenAt  atomic.Int64
}

// SubscriptionManager is the single owner of the dxLink feed subscription
// set: every change to what the server is subscribed to goes through here,
// and the in-memory counter map is kept in lockstep with the wire state.
// Observation is lock-free on the hot path.
type SubscriptionManager struct {
	fc    FeedController
	mu    sync.RWMutex
	state map[subKey]*subState
	now   func() time.Time
}

// NewSubscriptionManager constructs a manager bound to the supplied
// FeedController. The controller must already be connected and have its
// feed channel open (see dxclient.Client.OpenFeed) — Add/Remove just send
// incremental FEED_SUBSCRIPTION updates on that channel.
func NewSubscriptionManager(fc FeedController) *SubscriptionManager {
	return &SubscriptionManager{
		fc:    fc,
		state: map[subKey]*subState{},
		now:   time.Now,
	}
}

// BulkAdd starts a batch of subscriptions in a single wire call. Use for
// initial startup — one FEED_SUBSCRIPTION frame carrying all symbols is
// cheaper than N frames, and it keeps the per-item state writes
// contiguous under the manager lock. (The old rationale — race-avoiding
// an ack handler — evaporated once FEED_SUBSCRIPTION became
// fire-and-forget; efficiency is the only reason left.)
//
// Pairs already present are skipped silently (see Add for rationale).
func (m *SubscriptionManager) BulkAdd(pairs []struct{ Event, Symbol string }) error {
	if len(pairs) == 0 {
		return nil
	}
	fresh := make([]dxclient.FeedSub, 0, len(pairs))
	m.mu.Lock()
	for _, p := range pairs {
		k := subKey{event: p.Event, symbol: p.Symbol}
		if _, exists := m.state[k]; exists {
			continue
		}
		fresh = append(fresh, dxclient.FeedSub{Event: p.Event, Symbol: p.Symbol})
	}
	m.mu.Unlock()
	if len(fresh) == 0 {
		return nil
	}

	if err := m.fc.UpdateSubscription(fresh, nil); err != nil {
		return fmt.Errorf("subscriptions: bulk add: %w", err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sub := range fresh {
		k := subKey{event: sub.Event, symbol: sub.Symbol}
		if _, exists := m.state[k]; exists {
			continue
		}
		m.state[k] = &subState{subject: subjectFor(sub.Event, sub.Symbol)}
	}
	return nil
}

// Add starts a subscription on the wire and records local state. Idempotent:
// re-adding an already-registered (event, symbol) is a no-op and does not
// dispatch a duplicate FEED_SUBSCRIPTION (the server rejects duplicates and
// re-dispatch would clobber traffic counters).
func (m *SubscriptionManager) Add(event, symbol string) error {
	k := subKey{event: event, symbol: symbol}
	m.mu.Lock()
	if _, exists := m.state[k]; exists {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	if err := m.fc.UpdateSubscription(
		[]dxclient.FeedSub{{Event: event, Symbol: symbol}},
		nil,
	); err != nil {
		return fmt.Errorf("subscriptions: add %s/%s: %w", event, symbol, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Double-check under the write lock in case of a concurrent Add — the
	// second winner just drops its state write.
	if _, exists := m.state[k]; exists {
		return nil
	}
	m.state[k] = &subState{subject: subjectFor(event, symbol)}
	return nil
}

// Remove stops a subscription on the wire and drops local state. No-op if
// the key is unknown (dxLink would reject a "remove unknown" request).
func (m *SubscriptionManager) Remove(event, symbol string) error {
	k := subKey{event: event, symbol: symbol}
	m.mu.RLock()
	_, exists := m.state[k]
	m.mu.RUnlock()
	if !exists {
		return nil
	}

	if err := m.fc.UpdateSubscription(
		nil,
		[]dxclient.FeedSub{{Event: event, Symbol: symbol}},
	); err != nil {
		// Leave state in place: the wire rejected our remove, so the
		// server is still sending us events. Dropping state would make
		// the UI lie about what's active.
		return fmt.Errorf("subscriptions: remove %s/%s: %w", event, symbol, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.state, k)
	return nil
}

// Observe increments the msgCount and bumps lastSeenAt for (event, symbol).
// No-op for unregistered keys — the subject mapping comes from Add, not
// inbound traffic.
func (m *SubscriptionManager) Observe(event, symbol string) {
	k := subKey{event: event, symbol: symbol}
	m.mu.RLock()
	st, ok := m.state[k]
	m.mu.RUnlock()
	if !ok {
		return
	}
	nsec := m.now().UnixNano()
	st.msgCount.Add(1)
	if st.firstSeenAt.Load() == 0 {
		st.firstSeenAt.CompareAndSwap(0, nsec)
	}
	st.lastSeenAt.Store(nsec)
}

// Snapshot returns a sorted slice (by event, then symbol) of the current
// subscription state. Values are copies — mutating them doesn't affect the
// manager's internal state.
func (m *SubscriptionManager) Snapshot() []SubscriptionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SubscriptionInfo, 0, len(m.state))
	for k, st := range m.state {
		info := SubscriptionInfo{
			Event:    k.event,
			Symbol:   k.symbol,
			Subject:  st.subject,
			MsgCount: st.msgCount.Load(),
		}
		if n := st.firstSeenAt.Load(); n != 0 {
			info.FirstSeenAt = time.Unix(0, n)
		}
		if n := st.lastSeenAt.Load(); n != 0 {
			info.LastSeenAt = time.Unix(0, n)
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Event != out[j].Event {
			return out[i].Event < out[j].Event
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out
}
