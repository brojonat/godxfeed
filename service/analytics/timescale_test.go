package analytics

import (
	"testing"
	"time"
)

func TestParseQuoteInsert_Happy(t *testing.T) {
	ts := time.Unix(1_700_000_000, 0)
	payload := []byte(`{"eventType":"Quote","eventSymbol":"SPY","bidPrice":100.5,"askPrice":100.6,"bidSize":185,"askSize":200}`)
	p, err := parseQuoteInsert(payload, ts)
	if err != nil {
		t.Fatalf("parseQuoteInsert: %v", err)
	}
	if p.Symbol != "SPY" {
		t.Errorf("Symbol = %q", p.Symbol)
	}
	if p.BidPrice != 100.5 || p.AskPrice != 100.6 {
		t.Errorf("bid/ask = %v/%v, want 100.5/100.6", p.BidPrice, p.AskPrice)
	}
	if p.BidSize != 185 || p.AskSize != 200 {
		t.Errorf("sizes = %v/%v, want 185/200", p.BidSize, p.AskSize)
	}
	if !p.Ts.Time.Equal(ts) {
		t.Errorf("Ts.Time = %v, want %v", p.Ts.Time, ts)
	}
}

func TestParseQuoteInsert_RejectsNonQuote(t *testing.T) {
	payload := []byte(`{"eventType":"Greeks","eventSymbol":".SPY250321C500","delta":0.62}`)
	_, err := parseQuoteInsert(payload, time.Now())
	if err == nil {
		t.Fatal("expected error for non-Quote event; this sink only persists Quotes")
	}
}

func TestParseQuoteInsert_RejectsMissingSymbol(t *testing.T) {
	payload := []byte(`{"eventType":"Quote","bidPrice":1,"askPrice":2}`)
	_, err := parseQuoteInsert(payload, time.Now())
	if err == nil {
		t.Fatal("expected error when eventSymbol missing")
	}
}

func TestParseQuoteInsert_MalformedJSON(t *testing.T) {
	_, err := parseQuoteInsert([]byte(`not json`), time.Now())
	if err == nil {
		t.Fatal("expected error on malformed JSON")
	}
}

func TestParseQuoteInsert_AllowsOmittedEventType(t *testing.T) {
	// Some publishers (e.g. the dummy debug publisher) may send Quote-shaped
	// payloads without an eventType. We accept them — the symbol + price
	// fields are what actually matter for the insert.
	payload := []byte(`{"eventSymbol":"SPY","bidPrice":1.0,"askPrice":2.0}`)
	p, err := parseQuoteInsert(payload, time.Now())
	if err != nil {
		t.Fatalf("should tolerate omitted eventType; got %v", err)
	}
	if p.Symbol != "SPY" {
		t.Errorf("Symbol = %q", p.Symbol)
	}
}

// TestTimescaleSinkIsRegistered is a sanity check: the init() block must have
// registered both "postgres" and "postgresql".
func TestTimescaleSinkIsRegistered(t *testing.T) {
	schemes := RegisteredSchemes()
	want := map[string]bool{"postgres": false, "postgresql": false}
	for _, s := range schemes {
		if _, ok := want[s]; ok {
			want[s] = true
		}
	}
	for scheme, found := range want {
		if !found {
			t.Errorf("scheme %q not registered; registered schemes: %v", scheme, schemes)
		}
	}
}
