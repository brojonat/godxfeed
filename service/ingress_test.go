package service

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	dx "github.com/brojonat/godxfeed/dxclient"
)

func TestFeedEvents_NonFeedDataReturnsNilNil(t *testing.T) {
	cases := []dx.Message{
		dx.MessageKeepalive{MessageBase: dx.MessageBase{Type: dx.MESSAGE_TYPE_KEEPALIVE}},
		dx.MessageError{MessageBase: dx.MessageBase{Type: dx.MESSAGE_TYPE_ERROR}, Error: "TIMEOUT"},
		dx.MessageAuthState{MessageBase: dx.MessageBase{Type: dx.MESSAGE_TYPE_AUTH_STATE}, State: "AUTHORIZED"},
	}
	for _, msg := range cases {
		got, err := FeedEvents(msg)
		if err != nil {
			t.Errorf("%T: unexpected err: %v", msg, err)
		}
		if got != nil {
			t.Errorf("%T: expected nil events, got %v", msg, got)
		}
	}
}

func TestFeedEvents_ExtractsOnePerEvent(t *testing.T) {
	fd := dx.MessageFeedData{
		MessageBase: dx.MessageBase{Type: dx.MESSAGE_TYPE_FEED_DATA, Channel: 1},
		Data: []byte(`[
			{"eventType":"Quote","eventSymbol":"SPY","bidPrice":100.5,"askPrice":100.6},
			{"eventType":"Quote","eventSymbol":"AAPL","bidPrice":200.0,"askPrice":200.1}
		]`),
	}
	got, err := FeedEvents(fd)
	if err != nil {
		t.Fatalf("FeedEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}

	if got[0].Symbol != "SPY" || got[0].Subject != "godxfeed.SPY" || got[0].Event != "Quote" {
		t.Errorf("event[0] = %+v", got[0])
	}
	if got[1].Symbol != "AAPL" || got[1].Subject != "godxfeed.AAPL" {
		t.Errorf("event[1] = %+v", got[1])
	}

	// Payload should be valid JSON and contain the bid/ask fields verbatim —
	// i.e. we did not round-trip through a lossy struct.
	var decoded map[string]any
	if err := json.Unmarshal(got[0].Payload, &decoded); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if v, _ := decoded["bidPrice"].(float64); v != 100.5 {
		t.Errorf("bidPrice preserved? got %v", decoded["bidPrice"])
	}
	if v, _ := decoded["askPrice"].(float64); v != 100.6 {
		t.Errorf("askPrice preserved? got %v", decoded["askPrice"])
	}
}

func TestFeedEvents_SanitizesNaN(t *testing.T) {
	// dxfeed sends "NaN" (a JSON string) where a float is expected. Our
	// parser must turn that into 0.0 inline without failing.
	fd := dx.MessageFeedData{
		MessageBase: dx.MessageBase{Type: dx.MESSAGE_TYPE_FEED_DATA, Channel: 1},
		Data:        []byte(`[{"eventType":"Quote","eventSymbol":"SPY","bidPrice":"NaN","askPrice":100.6}]`),
	}
	got, err := FeedEvents(fd)
	if err != nil {
		t.Fatalf("FeedEvents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d", len(got))
	}

	// The payload should have the sanitized bytes.
	if bytes.Contains(got[0].Payload, []byte(`"NaN"`)) {
		t.Errorf("payload still contains quoted NaN: %s", got[0].Payload)
	}
	if !bytes.Contains(got[0].Payload, []byte(`0.0`)) {
		t.Errorf("payload should contain 0.0 after sanitization: %s", got[0].Payload)
	}
}

func TestFeedEvents_EmptyArrayYieldsNothing(t *testing.T) {
	fd := dx.MessageFeedData{Data: []byte(`[]`)}
	got, err := FeedEvents(fd)
	if err != nil {
		t.Fatalf("FeedEvents: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestFeedEvents_SkipsEventsWithoutSymbol(t *testing.T) {
	// Some dxlink event types (e.g. Configuration, Message) have no
	// eventSymbol. They're not routable to a per-symbol NATS subject so we
	// silently drop them.
	fd := dx.MessageFeedData{
		Data: []byte(`[
			{"eventType":"Configuration","some":"global"},
			{"eventType":"Quote","eventSymbol":"SPY","bidPrice":1}
		]`),
	}
	got, err := FeedEvents(fd)
	if err != nil {
		t.Fatalf("FeedEvents: %v", err)
	}
	if len(got) != 1 || got[0].Symbol != "SPY" {
		t.Errorf("expected only the SPY quote; got %+v", got)
	}
}

func TestFeedEvents_MalformedJSONIsError(t *testing.T) {
	fd := dx.MessageFeedData{Data: []byte(`not a JSON array`)}
	_, err := FeedEvents(fd)
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
	if !strings.Contains(err.Error(), "ingress") {
		t.Errorf("error should be scoped to ingress package: %v", err)
	}
}

func TestFeedEvents_MultipleEventTypes(t *testing.T) {
	// Simulates what Phase 3 will look like: a mixed batch with Quote +
	// Greeks + TheoPrice. Parser should emit all of them; current subject
	// scheme collapses them onto `godxfeed.<symbol>` — downstream consumers
	// will discriminate by eventType field inside the payload.
	fd := dx.MessageFeedData{
		Data: []byte(`[
			{"eventType":"Quote","eventSymbol":"SPY","bidPrice":580,"askPrice":580.05},
			{"eventType":"Greeks","eventSymbol":".SPY260321C500","delta":0.62,"gamma":0.01},
			{"eventType":"TheoPrice","eventSymbol":".SPY260321C500","price":81.2}
		]`),
	}
	got, err := FeedEvents(fd)
	if err != nil {
		t.Fatalf("FeedEvents: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantEvents := []string{"Quote", "Greeks", "TheoPrice"}
	for i, w := range wantEvents {
		if got[i].Event != w {
			t.Errorf("event[%d].Event = %q, want %q", i, got[i].Event, w)
		}
	}
}
