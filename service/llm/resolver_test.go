package llm

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/brojonat/godxfeed/service/api"
)

// fakeSource is a test double for SymbologySource. Each field is an
// optional override; unset behavior falls back to the canned chain so
// most tests can just flip the switches they care about.
type fakeSource struct {
	equity   string
	equityFn func(root string) (string, error)
	opts     []api.OptionSymbol
	optsFn   func(root string) ([]api.OptionSymbol, error)
	seenRoot string
}

func (f *fakeSource) EquityStreamerSymbol(root string) (string, error) {
	f.seenRoot = root
	if f.equityFn != nil {
		return f.equityFn(root)
	}
	if f.equity == "" {
		return root, nil
	}
	return f.equity, nil
}

func (f *fakeSource) OptionSymbols(root string) ([]api.OptionSymbol, error) {
	f.seenRoot = root
	if f.optsFn != nil {
		return f.optsFn(root)
	}
	return f.opts, nil
}

// mkOpt is a brief constructor for option metadata — the full
// OptionSymbol struct has 20+ fields but only 4 matter to the Resolver.
func mkOpt(streamer, expiry, strike, kind string) api.OptionSymbol {
	return api.OptionSymbol{
		StreamerSymbol: streamer,
		ExpirationDate: expiry,
		StrikePrice:    strike,
		OptionType:     kind,
	}
}

func TestResolve_RootRequired(t *testing.T) {
	r := &Resolver{Source: &fakeSource{}}
	if _, err := r.Resolve(FilterSpec{}); err == nil {
		t.Fatal("expected error on empty root")
	}
}

func TestResolve_BareEquity_DefaultsToQuoteAndIncludesUnderlying(t *testing.T) {
	// "Subscribe to SPY" — no option filters. Should produce exactly one
	// sub: (Quote, SPY_STREAMER). Even though include_equity is zero-
	// value false, the "pure equity" path flips it on so bare requests
	// don't silently return zero subs.
	src := &fakeSource{equity: "SPY_STREAMER"}
	r := &Resolver{Source: src}

	got, err := r.Resolve(FilterSpec{Root: "SPY"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 1 || got[0].Event != "Quote" || got[0].Symbol != "SPY_STREAMER" {
		t.Errorf("got %+v, want [{Quote, SPY_STREAMER}]", got)
	}
	if src.seenRoot != "SPY" {
		t.Errorf("source saw root %q, want SPY", src.seenRoot)
	}
}

func TestResolve_LowercaseRootIsNormalized(t *testing.T) {
	src := &fakeSource{equity: "SPY"}
	r := &Resolver{Source: src}
	if _, err := r.Resolve(FilterSpec{Root: "  spy  "}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if src.seenRoot != "SPY" {
		t.Errorf("source saw root %q, want uppercase SPY", src.seenRoot)
	}
}

func TestResolve_StrikeWindowFiltersOptions(t *testing.T) {
	// Calls on SPY, strikes 250..270 inclusive, single expiry.
	src := &fakeSource{
		equity: "SPY",
		opts: []api.OptionSymbol{
			mkOpt(".SPY260320C240", "2026-03-20", "240", "C"), // out (below)
			mkOpt(".SPY260320C250", "2026-03-20", "250", "C"), // in
			mkOpt(".SPY260320C260", "2026-03-20", "260", "C"), // in
			mkOpt(".SPY260320C270", "2026-03-20", "270", "C"), // in (inclusive)
			mkOpt(".SPY260320C280", "2026-03-20", "280", "C"), // out (above)
			mkOpt(".SPY260320P260", "2026-03-20", "260", "P"), // out (wrong kind)
		},
	}
	r := &Resolver{Source: src}

	got, err := r.Resolve(FilterSpec{
		Root:         "SPY",
		Kind:         "call",
		StrikeWindow: &Range{Min: 250, Max: 270},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := []string{".SPY260320C250", ".SPY260320C260", ".SPY260320C270"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (%+v)", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Symbol != w || got[i].Event != "Quote" {
			t.Errorf("sub[%d] = %+v, want (Quote, %s)", i, got[i], w)
		}
	}
}

func TestResolve_ExpiryWindowFiltersOptions(t *testing.T) {
	src := &fakeSource{
		opts: []api.OptionSymbol{
			mkOpt(".OLD", "2026-02-20", "500", "C"), // out (before window)
			mkOpt(".IN1", "2026-03-06", "500", "C"), // in
			mkOpt(".IN2", "2026-03-20", "500", "C"), // in
			mkOpt(".IN3", "2026-03-27", "500", "C"), // in (inclusive)
			mkOpt(".NEW", "2026-04-03", "500", "C"), // out (after window)
		},
	}
	r := &Resolver{Source: src}

	got, err := r.Resolve(FilterSpec{
		Root:         "SPY",
		Kind:         "call",
		ExpiryWindow: &DateWindow{Min: "2026-03-01", Max: "2026-03-31"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(got), got)
	}
	wantSyms := []string{".IN1", ".IN2", ".IN3"}
	for i, w := range wantSyms {
		if got[i].Symbol != w {
			t.Errorf("sub[%d] = %+v, want %s", i, got[i], w)
		}
	}
}

func TestResolve_KindBothReturnsCallsAndPuts(t *testing.T) {
	src := &fakeSource{
		opts: []api.OptionSymbol{
			mkOpt(".CALL", "2026-03-20", "500", "C"),
			mkOpt(".PUT", "2026-03-20", "500", "P"),
		},
	}
	r := &Resolver{Source: src}

	got, err := r.Resolve(FilterSpec{Root: "SPY", Kind: "both"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 (%+v)", len(got), got)
	}
}

func TestResolve_IncludeEquityWithOptions(t *testing.T) {
	src := &fakeSource{
		equity: "SPY",
		opts: []api.OptionSymbol{
			mkOpt(".CALL1", "2026-03-20", "500", "C"),
			mkOpt(".CALL2", "2026-03-20", "510", "C"),
		},
	}
	r := &Resolver{Source: src}

	got, err := r.Resolve(FilterSpec{
		Root:          "SPY",
		Kind:          "call",
		IncludeEquity: true,
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Order contract: equity first, options sorted after.
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(got), got)
	}
	if got[0].Symbol != "SPY" {
		t.Errorf("first sub should be equity; got %+v", got[0])
	}
}

func TestResolve_MultipleEventTypesFanOut(t *testing.T) {
	src := &fakeSource{
		opts: []api.OptionSymbol{
			mkOpt(".ONE", "2026-03-20", "500", "C"),
		},
	}
	r := &Resolver{Source: src}

	got, err := r.Resolve(FilterSpec{
		Root:       "SPY",
		Kind:       "call",
		EventTypes: []string{"Quote", "Greeks", "TheoPrice"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// 1 option × 3 event types → 3 subs.
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (%+v)", len(got), got)
	}
	gotEvents := map[string]bool{}
	for _, s := range got {
		if s.Symbol != ".ONE" {
			t.Errorf("wrong symbol: %+v", s)
		}
		gotEvents[s.Event] = true
	}
	for _, want := range []string{"Quote", "Greeks", "TheoPrice"} {
		if !gotEvents[want] {
			t.Errorf("missing event %s", want)
		}
	}
}

func TestResolve_EmptyEventTypesDefaultsToQuote(t *testing.T) {
	src := &fakeSource{equity: "SPY"}
	r := &Resolver{Source: src}
	got, err := r.Resolve(FilterSpec{Root: "SPY"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 1 || got[0].Event != "Quote" {
		t.Errorf("expected Quote default, got %+v", got)
	}
}

func TestResolve_InvalidKindErrors(t *testing.T) {
	r := &Resolver{Source: &fakeSource{}}
	if _, err := r.Resolve(FilterSpec{Root: "SPY", Kind: "CALL"}); err == nil {
		t.Error("want error for uppercase kind (case-sensitive contract)")
	}
	if _, err := r.Resolve(FilterSpec{Root: "SPY", Kind: "straddle"}); err == nil {
		t.Error("want error for unknown kind")
	}
}

func TestResolve_InvalidEventTypeErrors(t *testing.T) {
	r := &Resolver{Source: &fakeSource{}}
	_, err := r.Resolve(FilterSpec{Root: "SPY", EventTypes: []string{"NotReal"}})
	if err == nil {
		t.Fatal("want error for unknown event type")
	}
	if !strings.Contains(err.Error(), "NotReal") {
		t.Errorf("error should name the bad type: %v", err)
	}
}

func TestResolve_BadExpiryDatesError(t *testing.T) {
	r := &Resolver{Source: &fakeSource{opts: []api.OptionSymbol{{StreamerSymbol: "X", ExpirationDate: "2026-03-20", StrikePrice: "1"}}}}
	_, err := r.Resolve(FilterSpec{
		Root:         "SPY",
		Kind:         "call",
		ExpiryWindow: &DateWindow{Min: "2026-03-01", Max: "not-a-date"},
	})
	if err == nil {
		t.Fatal("want error for malformed expiry date")
	}
}

func TestResolve_InvertedExpiryWindowErrors(t *testing.T) {
	r := &Resolver{Source: &fakeSource{}}
	_, err := r.Resolve(FilterSpec{
		Root:         "SPY",
		Kind:         "call",
		ExpiryWindow: &DateWindow{Min: "2026-03-31", Max: "2026-03-01"},
	})
	if err == nil {
		t.Fatal("want error when expiry max < min")
	}
}

func TestResolve_InvertedStrikeWindowErrors(t *testing.T) {
	r := &Resolver{Source: &fakeSource{}}
	_, err := r.Resolve(FilterSpec{
		Root:         "SPY",
		Kind:         "call",
		StrikeWindow: &Range{Min: 300, Max: 200},
	})
	if err == nil {
		t.Fatal("want error when strike max < min")
	}
}

func TestResolve_SourceEquityErrorPropagates(t *testing.T) {
	boom := errors.New("tastytrade 500")
	src := &fakeSource{equityFn: func(root string) (string, error) { return "", boom }}
	r := &Resolver{Source: src}
	_, err := r.Resolve(FilterSpec{Root: "SPY", IncludeEquity: true})
	if !errors.Is(err, boom) {
		t.Errorf("want wrapped source err; got %v", err)
	}
}

func TestResolve_NoMatchingOptionsReturnsEmpty(t *testing.T) {
	// Empty option set + options-only filter → no subs, no error. It's
	// the caller's policy whether "nothing to subscribe" is a problem.
	r := &Resolver{Source: &fakeSource{opts: []api.OptionSymbol{mkOpt(".X", "2026-03-20", "500", "C")}}}
	got, err := r.Resolve(FilterSpec{
		Root:         "SPY",
		Kind:         "put", // nothing in the chain is a put
		ExpiryWindow: &DateWindow{Min: "2026-03-01", Max: "2026-03-31"},
	})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("want empty; got %+v", got)
	}
}

func TestResolve_SortOrderStable(t *testing.T) {
	// Scramble input, verify output is sorted by (expiry, strike, kind).
	src := &fakeSource{
		opts: []api.OptionSymbol{
			mkOpt(".B", "2026-03-20", "510", "P"),
			mkOpt(".A", "2026-03-20", "510", "C"),
			mkOpt(".D", "2026-03-27", "500", "C"),
			mkOpt(".C", "2026-03-20", "500", "C"),
		},
	}
	r := &Resolver{Source: src}
	got, err := r.Resolve(FilterSpec{Root: "SPY", Kind: "both"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	wantOrder := []string{".C", ".A", ".B", ".D"}
	if len(got) != len(wantOrder) {
		t.Fatalf("len = %d, want %d", len(got), len(wantOrder))
	}
	for i, w := range wantOrder {
		if got[i].Symbol != w {
			t.Errorf("sub[%d] = %s, want %s (got order: %v)", i, got[i].Symbol, w, symbolsOf(got))
		}
	}
}

func symbolsOf(ss []Sub) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%s/%s", s.Event, s.Symbol)
	}
	return out
}
