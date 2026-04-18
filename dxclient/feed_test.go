package dxclient_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brojonat/godxfeed/dxclient"
	"github.com/gorilla/websocket"
)

// fakeServer is a minimal dxlink-protocol stub: it replies to SETUP/AUTH to
// let the client connect, records every inbound message, and sends canned
// CHANNEL_OPENED + FEED_CONFIG responses so OpenFeed/UpdateSubscription can
// make progress without a real dxFeed endpoint.
type fakeServer struct {
	srv *httptest.Server
	mu  sync.Mutex
	// received holds every non-keepalive message received, newest last.
	received []rawMessage
}

type rawMessage struct {
	Type    string
	Channel int
	Payload []byte
}

var fakeUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func newFakeServer(t *testing.T) *fakeServer {
	t.Helper()
	fs := &fakeServer{}
	fs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := fakeUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		for {
			_, b, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var base dxclient.MessageBase
			if err := json.Unmarshal(b, &base); err != nil {
				continue
			}

			if base.Type != dxclient.MESSAGE_TYPE_KEEPALIVE {
				fs.mu.Lock()
				fs.received = append(fs.received, rawMessage{
					Type:    base.Type,
					Channel: base.Channel,
					Payload: append([]byte(nil), b...),
				})
				fs.mu.Unlock()
			}

			switch base.Type {
			case dxclient.MESSAGE_TYPE_SETUP:
				reply, _ := json.Marshal(dxclient.MessageSetup{
					MessageBase:            dxclient.MessageBase{Type: dxclient.MESSAGE_TYPE_SETUP, Channel: 0},
					KeepAliveTimeout:       60,
					AcceptKeepAliveTimeout: 60,
					Version:                "0.1",
				})
				conn.WriteMessage(websocket.TextMessage, reply)
			case dxclient.MESSAGE_TYPE_AUTH:
				reply, _ := json.Marshal(dxclient.MessageAuthState{
					MessageBase: dxclient.MessageBase{Type: dxclient.MESSAGE_TYPE_AUTH_STATE, Channel: 0},
					State:       dxclient.AUTH_STATE_AUTHORIZED,
				})
				conn.WriteMessage(websocket.TextMessage, reply)
			case dxclient.MESSAGE_TYPE_CHANNEL_REQUEST:
				reply, _ := json.Marshal(dxclient.MessageChannelOpened{
					MessageBase: dxclient.MessageBase{Type: dxclient.MESSAGE_TYPE_CHANNEL_OPENED, Channel: base.Channel},
					Service:     dxclient.CHANNEL_SERVICE_FEED,
					Parameters:  dxclient.FeedContract{Contract: dxclient.FEED_CONTRACT_STREAM},
				})
				conn.WriteMessage(websocket.TextMessage, reply)
			case dxclient.MESSAGE_TYPE_FEED_SETUP:
				reply, _ := json.Marshal(dxclient.MessageFeedConfig{
					MessageBase: dxclient.MessageBase{Type: dxclient.MESSAGE_TYPE_FEED_CONFIG, Channel: base.Channel},
					DataFormat:  dxclient.FEED_DATA_FORMAT_FULL,
				})
				conn.WriteMessage(websocket.TextMessage, reply)
			case dxclient.MESSAGE_TYPE_FEED_SUBSCRIPTION:
				// Server reply after a subscription change is a FEED_CONFIG
				// confirmation per the dxlink protocol.
				reply, _ := json.Marshal(dxclient.MessageFeedConfig{
					MessageBase: dxclient.MessageBase{Type: dxclient.MESSAGE_TYPE_FEED_CONFIG, Channel: base.Channel},
					DataFormat:  dxclient.FEED_DATA_FORMAT_FULL,
				})
				conn.WriteMessage(websocket.TextMessage, reply)
			}
		}
	}))
	t.Cleanup(fs.srv.Close)
	return fs
}

func (f *fakeServer) wsURL() string {
	return "ws" + strings.TrimPrefix(f.srv.URL, "http") + "/"
}

// wait up to 2s for a message of the given type to arrive; returns it or fails.
func (f *fakeServer) waitFor(t *testing.T, typ string) rawMessage {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, m := range f.received {
			if m.Type == typ {
				f.mu.Unlock()
				return m
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not receive %s within timeout; got %+v", typ, f.typesReceived())
	return rawMessage{}
}

func (f *fakeServer) typesReceived() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.received))
	for _, m := range f.received {
		out = append(out, m.Type)
	}
	return out
}

func (f *fakeServer) allOfType(typ string) []rawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []rawMessage{}
	for _, m := range f.received {
		if m.Type == typ {
			out = append(out, m)
		}
	}
	return out
}

// newDialedClient returns a client that's dialed and authenticated against fs.
func newDialedClient(t *testing.T, ctx context.Context, fs *fakeServer) dxclient.Client {
	t.Helper()
	c := dxclient.NewClient(func(lvl int, msg string, args ...any) {
		slog.Default().Log(ctx, slog.Level(lvl), msg, args...)
	})
	if err := c.Dial(ctx, fs.wsURL(), func(dxclient.MessageSetup) error { return nil }); err != nil {
		t.Fatalf("dial: %v", err)
	}
	if err := c.Authenticate("token"); err != nil {
		t.Fatalf("auth: %v", err)
	}
	return c
}

// TestOpenFeed_OpensChannelAndSendsFeedSetup drives the happy path: OpenFeed
// must send a CHANNEL_REQUEST (service=FEED) and then FEED_SETUP on the
// opened channel, and only return after FEED_CONFIG is acknowledged.
func TestOpenFeed_OpensChannelAndSendsFeedSetup(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newDialedClient(t, ctx, fs)

	if err := c.OpenFeed(); err != nil {
		t.Fatalf("OpenFeed: %v", err)
	}

	chReq := fs.waitFor(t, dxclient.MESSAGE_TYPE_CHANNEL_REQUEST)
	var req dxclient.MessageChannelRequest
	if err := json.Unmarshal(chReq.Payload, &req); err != nil {
		t.Fatalf("unmarshal channel request: %v", err)
	}
	if req.Service != dxclient.CHANNEL_SERVICE_FEED {
		t.Errorf("CHANNEL_REQUEST service = %q, want %q", req.Service, dxclient.CHANNEL_SERVICE_FEED)
	}
	if req.Channel <= 0 {
		t.Errorf("CHANNEL_REQUEST channel = %d, want > 0", req.Channel)
	}

	feedSetup := fs.waitFor(t, dxclient.MESSAGE_TYPE_FEED_SETUP)
	if feedSetup.Channel != req.Channel {
		t.Errorf("FEED_SETUP channel = %d, want %d", feedSetup.Channel, req.Channel)
	}
}

// TestUpdateSubscription_AddsSentOnFeedChannel verifies the incremental-add
// path: after OpenFeed, UpdateSubscription with adds must emit exactly one
// FEED_SUBSCRIPTION on the opened channel with the adds populated.
func TestUpdateSubscription_AddsSentOnFeedChannel(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newDialedClient(t, ctx, fs)

	if err := c.OpenFeed(); err != nil {
		t.Fatalf("OpenFeed: %v", err)
	}
	chReq := fs.waitFor(t, dxclient.MESSAGE_TYPE_CHANNEL_REQUEST)
	channel := chReq.Channel

	if err := c.UpdateSubscription(
		[]dxclient.FeedSub{{Event: "Quote", Symbol: "SPY"}, {Event: "Quote", Symbol: "AAPL"}},
		nil,
	); err != nil {
		t.Fatalf("UpdateSubscription: %v", err)
	}

	subs := fs.allOfType(dxclient.MESSAGE_TYPE_FEED_SUBSCRIPTION)
	if len(subs) != 1 {
		t.Fatalf("want exactly 1 FEED_SUBSCRIPTION, got %d", len(subs))
	}
	if subs[0].Channel != channel {
		t.Errorf("FEED_SUBSCRIPTION channel = %d, want %d (reuse OpenFeed channel)", subs[0].Channel, channel)
	}
	var ms dxclient.MessageFeedRegularSubscription
	if err := json.Unmarshal(subs[0].Payload, &ms); err != nil {
		t.Fatalf("unmarshal FEED_SUBSCRIPTION: %v", err)
	}
	if len(ms.Add) != 2 {
		t.Errorf("Add length = %d, want 2", len(ms.Add))
	}
	if len(ms.Remove) != 0 {
		t.Errorf("Remove length = %d, want 0", len(ms.Remove))
	}
	got := map[string]string{}
	for _, s := range ms.Add {
		got[string(s.Symbol)] = s.Type
	}
	if got["SPY"] != "Quote" || got["AAPL"] != "Quote" {
		t.Errorf("add payload wrong: %+v", ms.Add)
	}
}

// TestUpdateSubscription_RemovesSentOnFeedChannel: the remove branch of
// UpdateSubscription must populate FEED_SUBSCRIPTION.remove.
func TestUpdateSubscription_RemovesSentOnFeedChannel(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newDialedClient(t, ctx, fs)

	if err := c.OpenFeed(); err != nil {
		t.Fatalf("OpenFeed: %v", err)
	}
	// First add SPY so there's something to remove.
	if err := c.UpdateSubscription([]dxclient.FeedSub{{Event: "Quote", Symbol: "SPY"}}, nil); err != nil {
		t.Fatalf("UpdateSubscription add: %v", err)
	}
	if err := c.UpdateSubscription(nil, []dxclient.FeedSub{{Event: "Quote", Symbol: "SPY"}}); err != nil {
		t.Fatalf("UpdateSubscription remove: %v", err)
	}

	subs := fs.allOfType(dxclient.MESSAGE_TYPE_FEED_SUBSCRIPTION)
	if len(subs) != 2 {
		t.Fatalf("want 2 FEED_SUBSCRIPTION msgs (add then remove), got %d", len(subs))
	}
	var removeMsg dxclient.MessageFeedRegularSubscription
	if err := json.Unmarshal(subs[1].Payload, &removeMsg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(removeMsg.Remove) != 1 || removeMsg.Remove[0].Symbol != "SPY" {
		t.Errorf("remove payload = %+v, want single SPY Quote", removeMsg.Remove)
	}
	if len(removeMsg.Add) != 0 {
		t.Errorf("Add should be empty on pure-remove update; got %+v", removeMsg.Add)
	}
}

// TestUpdateSubscription_ReusesSameChannelAcrossCalls confirms the feed
// channel is opened once and all subsequent add/remove updates reuse it —
// this is the whole point of splitting OpenFeed from UpdateSubscription.
func TestUpdateSubscription_ReusesSameChannelAcrossCalls(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newDialedClient(t, ctx, fs)

	if err := c.OpenFeed(); err != nil {
		t.Fatalf("OpenFeed: %v", err)
	}
	for _, sym := range []string{"SPY", "AAPL", "QQQ"} {
		if err := c.UpdateSubscription([]dxclient.FeedSub{{Event: "Quote", Symbol: sym}}, nil); err != nil {
			t.Fatalf("UpdateSubscription %s: %v", sym, err)
		}
	}

	chReqs := fs.allOfType(dxclient.MESSAGE_TYPE_CHANNEL_REQUEST)
	if len(chReqs) != 1 {
		t.Fatalf("want exactly 1 CHANNEL_REQUEST across lifecycle, got %d", len(chReqs))
	}
	subs := fs.allOfType(dxclient.MESSAGE_TYPE_FEED_SUBSCRIPTION)
	if len(subs) != 3 {
		t.Fatalf("want 3 FEED_SUBSCRIPTION msgs, got %d", len(subs))
	}
	channel := chReqs[0].Channel
	for i, s := range subs {
		if s.Channel != channel {
			t.Errorf("FEED_SUBSCRIPTION #%d on channel %d, want %d", i, s.Channel, channel)
		}
	}
}

// TestUpdateSubscription_BeforeOpenFeedErrors: if the caller forgets to open
// the feed, UpdateSubscription must fail fast rather than silently sending
// FEED_SUBSCRIPTION on channel 0 (which is the control channel).
func TestUpdateSubscription_BeforeOpenFeedErrors(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newDialedClient(t, ctx, fs)

	err := c.UpdateSubscription([]dxclient.FeedSub{{Event: "Quote", Symbol: "SPY"}}, nil)
	if err == nil {
		t.Fatalf("UpdateSubscription before OpenFeed must error, got nil")
	}
	// Give the (bad) send a chance to arrive at the server if the guard is
	// missing, so we can assert nothing was emitted.
	time.Sleep(50 * time.Millisecond)
	if subs := fs.allOfType(dxclient.MESSAGE_TYPE_FEED_SUBSCRIPTION); len(subs) != 0 {
		t.Errorf("no FEED_SUBSCRIPTION should have been sent, got %d", len(subs))
	}
}

// TestOpenFeed_TwiceErrors: the feed channel is single-shot per connection.
// Opening twice should error rather than silently leaking a channel.
func TestOpenFeed_TwiceErrors(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c := newDialedClient(t, ctx, fs)

	if err := c.OpenFeed(); err != nil {
		t.Fatalf("first OpenFeed: %v", err)
	}
	if err := c.OpenFeed(); err == nil {
		t.Fatalf("second OpenFeed must error")
	}
}

// Sanity: confirm the fakeServer actually captures SETUP + AUTH, so test
// failures above can be trusted (i.e. the harness isn't silently broken).
func TestFakeServer_SeesSetupAndAuth(t *testing.T) {
	fs := newFakeServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = newDialedClient(t, ctx, fs)

	got := strings.Join(fs.typesReceived(), ",")
	for _, want := range []string{dxclient.MESSAGE_TYPE_SETUP, dxclient.MESSAGE_TYPE_AUTH} {
		if !strings.Contains(got, want) {
			t.Errorf("want %s in received types, got %s", want, got)
		}
	}
	_ = fmt.Sprintf // keep import
}
