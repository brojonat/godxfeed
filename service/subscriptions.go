package service

import (
	"sort"
	"sync"
	"sync/atomic"
	"time"
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

// SubscriptionManager tracks active dxLink subscriptions and per-(event,
// symbol) message counters. It is safe for concurrent use; observation is
// lock-free on the hot path.
type SubscriptionManager struct {
	mu    sync.RWMutex
	state map[subKey]*subState
	now   func() time.Time
}

// NewSubscriptionManager constructs a manager using time.Now as the clock.
func NewSubscriptionManager() *SubscriptionManager {
	return &SubscriptionManager{
		state: map[subKey]*subState{},
		now:   time.Now,
	}
}

// Register records an (event, symbol) subscription → NATS subject mapping.
// Idempotent: calling twice with the same key leaves state (including counters)
// unchanged — otherwise we'd clobber real traffic stats the first time a
// caller "re-registers" an already-active subscription.
func (m *SubscriptionManager) Register(event, symbol, subject string) {
	k := subKey{event: event, symbol: symbol}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.state[k]; exists {
		return
	}
	m.state[k] = &subState{subject: subject}
}

// Unregister removes a subscription. No-op if the key is unknown.
func (m *SubscriptionManager) Unregister(event, symbol string) {
	k := subKey{event: event, symbol: symbol}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.state, k)
}

// Observe increments the msgCount and bumps lastSeenAt for (event, symbol).
// If the subscription isn't registered, this is a no-op — Observe never
// auto-registers because the subject mapping comes from the dxLink subscribe
// call, not from inbound traffic.
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
	// CAS firstSeenAt from 0 only (first observation wins).
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
