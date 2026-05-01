package service

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/brojonat/godxfeed/dxclient"
)

// fakeFeedController is a test double for the dxclient side of
// SubscriptionManager. Each UpdateSubscription call is recorded; callers
// inject err to exercise the error path.
type fakeFeedController struct {
	mu    sync.Mutex
	calls []updateCall
	err   error
}

type updateCall struct {
	add    []dxclient.FeedSub
	remove []dxclient.FeedSub
}

func (f *fakeFeedController) UpdateSubscription(add, remove []dxclient.FeedSub) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	// Copy to detach from caller mutation.
	ac := append([]dxclient.FeedSub(nil), add...)
	rc := append([]dxclient.FeedSub(nil), remove...)
	f.calls = append(f.calls, updateCall{add: ac, remove: rc})
	return nil
}

func (f *fakeFeedController) Calls() []updateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]updateCall, len(f.calls))
	copy(out, f.calls)
	return out
}

// TestSubscriptionManager_AddDispatchesAndRegisters — the happy path: Add
// both sends the add to the dxclient AND makes the subscription visible in
// the snapshot. If either half is skipped, the system is broken.
func TestSubscriptionManager_AddDispatchesAndRegisters(t *testing.T) {
	fc := &fakeFeedController{}
	m := NewSubscriptionManager(fc)

	if err := m.Add("Quote", "SPY"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	calls := fc.Calls()
	if len(calls) != 1 {
		t.Fatalf("want 1 UpdateSubscription call, got %d", len(calls))
	}
	if len(calls[0].add) != 1 || calls[0].add[0].Symbol != "SPY" || calls[0].add[0].Event != "Quote" {
		t.Errorf("add payload wrong: %+v", calls[0].add)
	}
	if len(calls[0].remove) != 0 {
		t.Errorf("remove should be empty on Add; got %+v", calls[0].remove)
	}

	snap := m.Snapshot()
	if len(snap) != 1 || snap[0].Symbol != "SPY" || snap[0].Event != "Quote" {
		t.Errorf("snapshot wrong: %+v", snap)
	}
	if snap[0].Subject != "godxfeed.quote.SPY" {
		t.Errorf("subject = %q, want %q", snap[0].Subject, "godxfeed.quote.SPY")
	}
}

// TestSubscriptionManager_AddRollsBackOnClientError — if the dxclient
// reports a failure, state must NOT be updated. Otherwise Snapshot would
// show subscriptions that don't actually exist on the wire.
func TestSubscriptionManager_AddRollsBackOnClientError(t *testing.T) {
	fc := &fakeFeedController{err: errors.New("boom")}
	m := NewSubscriptionManager(fc)

	if err := m.Add("Quote", "SPY"); err == nil {
		t.Fatalf("Add must return error from client")
	}
	if snap := m.Snapshot(); len(snap) != 0 {
		t.Errorf("snapshot must be empty when client errored; got %+v", snap)
	}
}

// TestSubscriptionManager_AddIsIdempotent — re-adding an existing sub must
// not re-dispatch to the wire (the dxLink server would reject duplicate
// adds) and must not clobber existing message counts.
func TestSubscriptionManager_AddIsIdempotent(t *testing.T) {
	fc := &fakeFeedController{}
	m := NewSubscriptionManager(fc)

	if err := m.Add("Quote", "SPY"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	m.Observe("Quote", "SPY")
	m.Observe("Quote", "SPY")
	if err := m.Add("Quote", "SPY"); err != nil {
		t.Fatalf("Add (second): %v", err)
	}

	if got := len(fc.Calls()); got != 1 {
		t.Errorf("UpdateSubscription called %d times, want 1", got)
	}
	if snap := m.Snapshot(); snap[0].MsgCount != 2 {
		t.Errorf("msgCount clobbered by duplicate Add: got %d want 2", snap[0].MsgCount)
	}
}

// TestSubscriptionManager_RemoveDispatchesAndUnregisters — inverse of Add.
func TestSubscriptionManager_RemoveDispatchesAndUnregisters(t *testing.T) {
	fc := &fakeFeedController{}
	m := NewSubscriptionManager(fc)

	if err := m.Add("Quote", "SPY"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := m.Remove("Quote", "SPY"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	calls := fc.Calls()
	if len(calls) != 2 {
		t.Fatalf("want 2 UpdateSubscription calls (add, remove), got %d", len(calls))
	}
	if len(calls[1].remove) != 1 || calls[1].remove[0].Symbol != "SPY" {
		t.Errorf("remove payload wrong: %+v", calls[1].remove)
	}

	if snap := m.Snapshot(); len(snap) != 0 {
		t.Errorf("after Remove snapshot should be empty; got %+v", snap)
	}
}

// TestSubscriptionManager_RemoveMissingIsNoOp — removing something that
// isn't registered must not dispatch to the wire (the dxLink server would
// reject an unknown remove) and must not error.
func TestSubscriptionManager_RemoveMissingIsNoOp(t *testing.T) {
	fc := &fakeFeedController{}
	m := NewSubscriptionManager(fc)

	if err := m.Remove("Quote", "NOPE"); err != nil {
		t.Fatalf("Remove on missing must not error, got %v", err)
	}
	if got := len(fc.Calls()); got != 0 {
		t.Errorf("UpdateSubscription called %d times, want 0", got)
	}
}

// TestSubscriptionManager_RemoveLeavesStateWhenClientFails — if the wire
// side refuses the remove, local state must stay registered, otherwise the
// UI would show the sub as gone while messages keep arriving.
func TestSubscriptionManager_RemoveLeavesStateWhenClientFails(t *testing.T) {
	fc := &fakeFeedController{}
	m := NewSubscriptionManager(fc)
	if err := m.Add("Quote", "SPY"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	fc.mu.Lock()
	fc.err = errors.New("downstream failure")
	fc.mu.Unlock()

	if err := m.Remove("Quote", "SPY"); err == nil {
		t.Fatalf("Remove must surface client error")
	}
	if snap := m.Snapshot(); len(snap) != 1 {
		t.Errorf("state must be retained when Remove fails; got %+v", snap)
	}
}

// TestSubscriptionManager_ObserveIncrementsAndTimestamps — unchanged
// semantics from the pre-refactor manager, exercised via the new public
// API so the test survives the Register→Add rename.
func TestSubscriptionManager_ObserveIncrementsAndTimestamps(t *testing.T) {
	fc := &fakeFeedController{}
	m := NewSubscriptionManager(fc)
	clock := time.Unix(1_700_000_000, 0)
	m.now = func() time.Time { return clock }

	if err := m.Add("Quote", "SPY"); err != nil {
		t.Fatalf("Add: %v", err)
	}

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
	if got := info.FirstSeenAt.Unix(); got != 1_700_000_000 {
		t.Errorf("firstSeenAt unix = %d, want 1700000000", got)
	}
	if got := info.LastSeenAt.Unix(); got != 1_700_000_010 {
		t.Errorf("lastSeenAt unix = %d, want 1700000010", got)
	}
}

// TestSubscriptionManager_ObserveUnregisteredIsNoOp — Observe on an
// unknown key must never auto-register (the subject mapping comes from Add,
// not inbound traffic).
func TestSubscriptionManager_ObserveUnregisteredIsNoOp(t *testing.T) {
	m := NewSubscriptionManager(&fakeFeedController{})
	m.Observe("Quote", "SPY")
	if snap := m.Snapshot(); len(snap) != 0 {
		t.Errorf("Observe must not auto-register; got %+v", snap)
	}
}

// TestSubscriptionManager_ConcurrentObserveIsSafe — atomic counters under
// contention. Not ideology, just lock-free correctness.
func TestSubscriptionManager_ConcurrentObserveIsSafe(t *testing.T) {
	m := NewSubscriptionManager(&fakeFeedController{})
	if err := m.Add("Quote", "SPY"); err != nil {
		t.Fatalf("Add: %v", err)
	}

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

// TestSubscriptionManager_RebindResubscribes — Rebind swaps the controller
// and replays all active subscriptions to the new controller.
func TestSubscriptionManager_RebindResubscribes(t *testing.T) {
	fc1 := &fakeFeedController{}
	m := NewSubscriptionManager(fc1)

	m.Add("Quote", "SPY")
	m.Add("Quote", "AAPL")
	m.Add("Greeks", "SPY")
	m.Observe("Quote", "SPY")
	m.Observe("Quote", "SPY")

	fc2 := &fakeFeedController{}
	if err := m.Rebind(fc2); err != nil {
		t.Fatalf("Rebind: %v", err)
	}

	// fc2 should have received a single UpdateSubscription with all 3 subs.
	calls := fc2.Calls()
	if len(calls) != 1 {
		t.Fatalf("want 1 call on new controller, got %d", len(calls))
	}
	if len(calls[0].add) != 3 {
		t.Errorf("want 3 adds, got %d", len(calls[0].add))
	}

	// Msg counts should be preserved.
	snap := m.Snapshot()
	for _, s := range snap {
		if s.Event == "Quote" && s.Symbol == "SPY" && s.MsgCount != 2 {
			t.Errorf("SPY msgCount = %d, want 2 (preserved across Rebind)", s.MsgCount)
		}
	}

	// New Add should go to fc2, not fc1.
	if err := m.Add("Quote", "QQQ"); err != nil {
		t.Fatalf("Add after Rebind: %v", err)
	}
	if got := len(fc2.Calls()); got != 2 {
		t.Errorf("fc2 call count = %d, want 2 (rebind + new add)", got)
	}
	if got := len(fc1.Calls()); got != 3 {
		t.Errorf("fc1 call count = %d, want 3 (unchanged after rebind)", got)
	}
}

// TestSubscriptionManager_RebindEmptyState — Rebind with no subscriptions
// should swap the controller without any wire call.
func TestSubscriptionManager_RebindEmptyState(t *testing.T) {
	fc1 := &fakeFeedController{}
	m := NewSubscriptionManager(fc1)

	fc2 := &fakeFeedController{}
	if err := m.Rebind(fc2); err != nil {
		t.Fatalf("Rebind: %v", err)
	}
	if got := len(fc2.Calls()); got != 0 {
		t.Errorf("want 0 calls on empty rebind, got %d", got)
	}
}

// TestSubscriptionManager_RebindWireError — if the re-subscribe wire call
// fails, Rebind returns an error. The controller is still swapped (the old
// one is dead) so subsequent Adds go to the new controller.
func TestSubscriptionManager_RebindWireError(t *testing.T) {
	fc1 := &fakeFeedController{}
	m := NewSubscriptionManager(fc1)
	m.Add("Quote", "SPY")

	fc2 := &fakeFeedController{err: errors.New("wire error")}
	if err := m.Rebind(fc2); err == nil {
		t.Fatal("Rebind must surface wire error")
	}

	// fc should be swapped even on error.
	fc2.mu.Lock()
	fc2.err = nil
	fc2.mu.Unlock()
	if err := m.Add("Quote", "AAPL"); err != nil {
		t.Fatalf("Add after failed Rebind: %v", err)
	}
	if got := len(fc2.Calls()); got != 1 {
		t.Errorf("want 1 call on fc2 after retry, got %d", got)
	}
}

// TestSubscriptionManager_SortOrder — Snapshot order contract is stable.
func TestSubscriptionManager_SortOrder(t *testing.T) {
	m := NewSubscriptionManager(&fakeFeedController{})
	for _, p := range []struct{ ev, sym string }{
		{"Quote", "ZZZ"}, {"Greeks", "AAA"}, {"Quote", "AAA"}, {"Greeks", "ZZZ"},
	} {
		if err := m.Add(p.ev, p.sym); err != nil {
			t.Fatalf("Add %s/%s: %v", p.ev, p.sym, err)
		}
	}

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
