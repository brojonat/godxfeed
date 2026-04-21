package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	dx "github.com/brojonat/godxfeed/dxclient"
)

// FeedEvent is one event ready to publish: the NATS subject (derived from the
// event's type and symbol) and the raw JSON payload. The Event / Symbol
// fields are a convenience for counter bookkeeping — callers that only
// publish can ignore them.
//
// Payload is a slice into the caller's buffer after NaN sanitization; it is
// safe to publish immediately but must be copied before being retained past
// the next parse call.
type FeedEvent struct {
	Subject string
	Payload json.RawMessage
	Event   string
	Symbol  string
}

// SubjectFor returns the NATS subject for an (event, symbol) pair. The scheme
// is `godxfeed.<event>.<symbol>` with the event type lowercased. Consumers
// that want a specific event type subscribe to `godxfeed.<event>.>`; the
// legacy single-token `godxfeed.*` pattern no longer matches anything.
//
// Exported so HTTP handlers (e.g. /nl-subscribe) can synthesize the same
// subject the ingress will publish under, without re-implementing the scheme.
func SubjectFor(event, symbol string) string {
	return "godxfeed." + strings.ToLower(event) + "." + symbol
}

// FeedEvents converts a dxLink message into zero or more FeedEvents ready for
// NATS publication. Non-FEED_DATA messages return (nil, nil). The only
// allocations are:
//
//  1. one []json.RawMessage to hold the top-level array (N elements)
//  2. one tiny struct per element to pull out eventType / eventSymbol
//
// No full-fidelity decode of each event is performed — the raw bytes pass
// through to NATS untouched so downstream consumers can unmarshal whatever
// fields they care about (Quote fields, Greeks fields, etc.).
func FeedEvents(msg dx.Message) ([]FeedEvent, error) {
	fd, ok := msg.(dx.MessageFeedData)
	if !ok {
		return nil, nil
	}
	return parseFeedData(fd.Data)
}

// parseFeedData is the pure function version, easier to unit-test without
// constructing full dx.Message values.
func parseFeedData(data json.RawMessage) ([]FeedEvent, error) {
	if len(data) == 0 {
		return nil, nil
	}
	// dxfeed sometimes sends "NaN" where a float is expected; json.Unmarshal
	// chokes on it. Byte-level replace is cheap and correct (the string
	// literal "NaN" should never appear in a numeric field's valid value).
	sanitized := bytes.ReplaceAll(data, []byte(`"NaN"`), []byte(`0.0`))

	var events []json.RawMessage
	if err := json.Unmarshal(sanitized, &events); err != nil {
		return nil, fmt.Errorf("ingress: parse data array: %w", err)
	}

	out := make([]FeedEvent, 0, len(events))
	for _, raw := range events {
		var hdr struct {
			EventType   string `json:"eventType"`
			EventSymbol string `json:"eventSymbol"`
		}
		if err := json.Unmarshal(raw, &hdr); err != nil {
			return nil, fmt.Errorf("ingress: parse event header: %w", err)
		}
		if hdr.EventSymbol == "" {
			continue
		}
		out = append(out, FeedEvent{
			Subject: SubjectFor(hdr.EventType, hdr.EventSymbol),
			Payload: raw,
			Event:   hdr.EventType,
			Symbol:  hdr.EventSymbol,
		})
	}
	return out, nil
}
