package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brojonat/godxfeed/service"
	sapi "github.com/brojonat/godxfeed/service/api"
	"github.com/brojonat/godxfeed/service/llm"
	"github.com/nats-io/nats.go"
)

// fakeService stubs just the slice of service.Service the
// /nl-subscribe handler actually calls. Other methods panic so an
// accidental new dependency in the handler shows up as a loud test
// failure rather than silent zero-value behavior.
type fakeService struct {
	gotSymbolData func(symbol, symbolType string) (*sapi.Response, error)
	gotBulkAdd    func(pairs []struct{ Event, Symbol string }) error
	bulkAddErr    error
	bulkAddCalls  [][]struct{ Event, Symbol string }
}

func (f *fakeService) Log(level int, m string, args ...any) {}
func (f *fakeService) NATS() *nats.Conn                     { return nil }
func (f *fakeService) NewStreamerToken(ctx context.Context) (sapi.TokenData, error) {
	panic("unused")
}
func (f *fakeService) GetSymbolData(symbol, symbolType string) (*sapi.Response, error) {
	return f.gotSymbolData(symbol, symbolType)
}
func (f *fakeService) GetOptionChain(symbol string) (*sapi.Response, error) { panic("unused") }
func (f *fakeService) GetRelatedOptionSymbols(symbol string) ([]string, error) {
	panic("unused")
}
func (f *fakeService) GetStreamSymbols(symbol string, method func(string) ([]string, error)) ([]string, error) {
	panic("unused")
}
func (f *fakeService) StartIngress(ctx context.Context, symbols []string) error { panic("unused") }
func (f *fakeService) AddSubscription(event, symbol string) error               { panic("unused") }
func (f *fakeService) RemoveSubscription(event, symbol string) error            { panic("unused") }
func (f *fakeService) BulkAddSubscriptions(pairs []struct{ Event, Symbol string }) error {
	f.bulkAddCalls = append(f.bulkAddCalls, pairs)
	if f.bulkAddErr != nil {
		return f.bulkAddErr
	}
	if f.gotBulkAdd != nil {
		return f.gotBulkAdd(pairs)
	}
	return nil
}
func (f *fakeService) Subscriptions() []service.SubscriptionInfo { panic("unused") }
func (f *fakeService) DXLinkStatus() service.DXLinkStatus        { panic("unused") }

// stubProvider is an llm.Provider that returns a canned FilterSpec
// (or error). We don't want to call out to real LLMs in unit tests —
// the Provider implementations have their own wire-level tests.
type stubProvider struct {
	name string
	spec llm.FilterSpec
	err  error
	seen string
}

func (s *stubProvider) Name() string { return s.name }
func (s *stubProvider) Extract(ctx context.Context, userText string, now time.Time) (llm.FilterSpec, error) {
	s.seen = userText
	return s.spec, s.err
}

// tastySymbolResp is a minimal tastytrade /option-chains/SYM response
// body — the adapter only reads `items`, so everything else can be
// omitted.
func tastySymbolResp(items []sapi.OptionSymbol) *sapi.Response {
	payload := map[string]any{"items": items}
	data, _ := json.Marshal(payload)
	return &sapi.Response{Data: data}
}

func tastyEquityResp(symbol, streamer string) *sapi.Response {
	eq := sapi.EquitySymbol{Symbol: symbol, StreamerSymbol: streamer}
	data, _ := json.Marshal(eq)
	return &sapi.Response{Data: data}
}

func TestHandleNLSubscribe_HappyPath(t *testing.T) {
	// End-to-end through the handler: LLM stub returns a FilterSpec,
	// adapter returns equity + option chain, resolver filters,
	// BulkAddSubscriptions receives the concrete pairs, response echoes
	// everything back.
	svc := &fakeService{
		gotSymbolData: func(symbol, symbolType string) (*sapi.Response, error) {
			switch symbolType {
			case sapi.SYMBOL_TYPE_EQUITIES:
				return tastyEquityResp("SPY", "SPY"), nil
			case sapi.SYMBOL_TYPE_OPTIONS:
				return tastySymbolResp([]sapi.OptionSymbol{
					{StreamerSymbol: ".SPY260320C250", ExpirationDate: "2026-03-20", StrikePrice: "250", OptionType: "C"},
					{StreamerSymbol: ".SPY260320C260", ExpirationDate: "2026-03-20", StrikePrice: "260", OptionType: "C"},
					{StreamerSymbol: ".SPY260320P250", ExpirationDate: "2026-03-20", StrikePrice: "250", OptionType: "P"}, // filtered out (put)
				}), nil
			}
			return nil, fmt.Errorf("unexpected symbolType %q", symbolType)
		},
	}
	stub := &stubProvider{
		name: "anthropic",
		spec: llm.FilterSpec{
			Root:          "SPY",
			EventTypes:    []string{"Quote"},
			Kind:          "call",
			StrikeWindow:  &llm.Range{Min: 250, Max: 260},
			IncludeEquity: true,
		},
	}
	reg := ProviderRegistry{
		Providers: map[string]llm.Provider{"anthropic": stub},
		Default:   "anthropic",
	}
	h := handleNLSubscribe(svc, reg)

	req := httptest.NewRequest("POST", "/nl-subscribe",
		strings.NewReader(`{"text":"SPY calls 250-260, include stock"}`))
	rec := httptest.NewRecorder()
	h(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var out nlSubscribeResponse
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Provider != "anthropic" {
		t.Errorf("provider echo = %s", out.Provider)
	}
	if stub.seen != "SPY calls 250-260, include stock" {
		t.Errorf("provider didn't see raw text: %q", stub.seen)
	}
	// Equity + 2 calls = 3 subs. Puts were filtered out.
	if len(out.Subs) != 3 {
		t.Fatalf("subs = %d (%v), want 3", len(out.Subs), out.Subs)
	}
	// Equity first per Resolver order contract.
	if out.Subs[0].Symbol != "SPY" || out.Subs[0].Event != "Quote" {
		t.Errorf("subs[0] = %+v, want (Quote, SPY)", out.Subs[0])
	}
	// Subject format must match Phase 3 scheme.
	if out.Subs[0].Subject != "godxfeed.quote.SPY" {
		t.Errorf("subs[0].Subject = %s, want godxfeed.quote.SPY", out.Subs[0].Subject)
	}
	// BulkAdd dispatched exactly once with all three pairs.
	if len(svc.bulkAddCalls) != 1 {
		t.Fatalf("BulkAdd called %d times, want 1", len(svc.bulkAddCalls))
	}
	if len(svc.bulkAddCalls[0]) != 3 {
		t.Errorf("BulkAdd batch size = %d, want 3", len(svc.bulkAddCalls[0]))
	}
}

func TestHandleNLSubscribe_NoProvidersRegisteredReturns503(t *testing.T) {
	h := handleNLSubscribe(&fakeService{}, ProviderRegistry{})
	req := httptest.NewRequest("POST", "/nl-subscribe", strings.NewReader(`{"text":"SPY"}`))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", rec.Code)
	}
}

func TestHandleNLSubscribe_EmptyTextReturns400(t *testing.T) {
	reg := ProviderRegistry{
		Providers: map[string]llm.Provider{"anthropic": &stubProvider{name: "anthropic"}},
		Default:   "anthropic",
	}
	h := handleNLSubscribe(&fakeService{}, reg)
	req := httptest.NewRequest("POST", "/nl-subscribe", strings.NewReader(`{"text":""}`))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleNLSubscribe_UnknownProviderReturns400(t *testing.T) {
	reg := ProviderRegistry{
		Providers: map[string]llm.Provider{"anthropic": &stubProvider{name: "anthropic"}},
		Default:   "anthropic",
	}
	h := handleNLSubscribe(&fakeService{}, reg)
	req := httptest.NewRequest("POST", "/nl-subscribe",
		strings.NewReader(`{"text":"SPY","provider":"does-not-exist"}`))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleNLSubscribe_ProviderErrorReturns502(t *testing.T) {
	// LLM call failures are upstream problems, not client problems.
	// 502 Bad Gateway matches the convention for "I tried to talk to
	// an upstream and it didn't work."
	reg := ProviderRegistry{
		Providers: map[string]llm.Provider{
			"anthropic": &stubProvider{name: "anthropic", err: fmt.Errorf("rate limited")},
		},
		Default: "anthropic",
	}
	h := handleNLSubscribe(&fakeService{}, reg)
	req := httptest.NewRequest("POST", "/nl-subscribe", strings.NewReader(`{"text":"SPY"}`))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestHandleNLSubscribe_PicksNonDefaultProvider(t *testing.T) {
	// When body specifies provider, that one is used regardless of
	// the registry's Default. Lets callers A/B test providers.
	anth := &stubProvider{name: "anthropic", spec: llm.FilterSpec{Root: "SPY"}}
	oai := &stubProvider{name: "openai", spec: llm.FilterSpec{Root: "AAPL"}}
	svc := &fakeService{
		gotSymbolData: func(sym, typ string) (*sapi.Response, error) {
			return tastyEquityResp(sym, sym), nil
		},
	}
	reg := ProviderRegistry{
		Providers: map[string]llm.Provider{"anthropic": anth, "openai": oai},
		Default:   "anthropic",
	}
	h := handleNLSubscribe(svc, reg)
	body, _ := json.Marshal(map[string]string{"text": "whatever", "provider": "openai"})
	req := httptest.NewRequest("POST", "/nl-subscribe", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	// OpenAI stub saw the text; Anthropic didn't.
	if oai.seen == "" {
		t.Error("openai provider should have been invoked")
	}
	if anth.seen != "" {
		t.Error("anthropic provider should NOT have been invoked")
	}
}
