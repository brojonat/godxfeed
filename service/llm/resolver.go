package llm

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/brojonat/godxfeed/service/api"
)

// Sub is a concrete (event, symbol) subscription pair. Matches the
// shape SubscriptionManager.BulkAdd expects — it's declared here so
// the llm package doesn't import service, keeping the dependency
// arrow pointing one way.
type Sub struct {
	Event  string
	Symbol string
}

// SymbologySource is the tiny slice of tastytrade REST the Resolver
// needs. Split out as its own interface so tests can plug in a fake
// without standing up an HTTP server.
type SymbologySource interface {
	// EquityStreamerSymbol returns the dxLink streamer symbol for an
	// equity (e.g. EquityStreamerSymbol("SPY") → "SPY").
	EquityStreamerSymbol(root string) (string, error)

	// OptionSymbols returns every option-chain entry for root. The
	// Resolver filters this list by expiry/strike/kind in-process —
	// the source just returns what tastytrade has.
	OptionSymbols(root string) ([]api.OptionSymbol, error)
}

// Resolver expands a FilterSpec into concrete Subs by looking up
// symbology and filtering. Safe for concurrent use.
type Resolver struct {
	Source SymbologySource
}

// Resolve returns the Subs implied by f. The list order is stable:
// equity first (if any), then options sorted by (expiry, strike,
// kind) so downstream consumers get deterministic output.
//
// Validation rules:
//   - root is required.
//   - event_types defaults to ["Quote"] when empty.
//   - kind must be "", "call", "put", or "both".
//   - a non-empty kind with no matching options returns ([]Sub{}, nil)
//     — the caller can decide whether empty is an error.
func (r *Resolver) Resolve(f FilterSpec) ([]Sub, error) {
	f.Root = strings.ToUpper(strings.TrimSpace(f.Root))
	if f.Root == "" {
		return nil, fmt.Errorf("resolve: root is required")
	}
	if len(f.EventTypes) == 0 {
		f.EventTypes = []string{"Quote"}
	}
	if err := validateEventTypes(f.EventTypes); err != nil {
		return nil, err
	}
	switch f.Kind {
	case "", "call", "put", "both":
		// ok
	default:
		return nil, fmt.Errorf("resolve: invalid kind %q (want call|put|both|\"\")", f.Kind)
	}

	// Equity first.
	out := []Sub{}
	// include_equity defaults to true in the "pure equity" case (no
	// option filters) so a bare "subscribe to SPY" doesn't silently
	// resolve to zero subs. If the caller wants options-only, they
	// pass kind="call"/"put"/"both" and include_equity=false.
	wantEquity := f.IncludeEquity || (f.Kind == "" && f.StrikeWindow == nil && f.ExpiryWindow == nil)
	if wantEquity {
		streamer, err := r.Source.EquityStreamerSymbol(f.Root)
		if err != nil {
			return nil, fmt.Errorf("resolve: equity symbol %s: %w", f.Root, err)
		}
		for _, ev := range f.EventTypes {
			out = append(out, Sub{Event: ev, Symbol: streamer})
		}
	}

	// Options, if any filter asked for them.
	if f.Kind == "" && f.StrikeWindow == nil && f.ExpiryWindow == nil {
		return out, nil
	}
	opts, err := r.Source.OptionSymbols(f.Root)
	if err != nil {
		return nil, fmt.Errorf("resolve: option chain %s: %w", f.Root, err)
	}
	filtered, err := filterOptions(opts, f)
	if err != nil {
		return nil, err
	}
	for _, o := range filtered {
		for _, ev := range f.EventTypes {
			out = append(out, Sub{Event: ev, Symbol: o.StreamerSymbol})
		}
	}
	return out, nil
}

func validateEventTypes(ev []string) error {
	allowed := map[string]bool{"Quote": true, "Greeks": true, "TheoPrice": true, "Underlying": true}
	for _, e := range ev {
		if !allowed[e] {
			return fmt.Errorf("resolve: unknown event type %q", e)
		}
	}
	return nil
}

// filterOptions returns the subset of opts that matches f's windows and
// kind. Always returns opts sorted by (expiry, strike, kind) for
// deterministic output.
func filterOptions(opts []api.OptionSymbol, f FilterSpec) ([]api.OptionSymbol, error) {
	var expMin, expMax time.Time
	if f.ExpiryWindow != nil {
		var err error
		expMin, err = time.Parse("2006-01-02", f.ExpiryWindow.Min)
		if err != nil {
			return nil, fmt.Errorf("resolve: bad expiry min %q: %w", f.ExpiryWindow.Min, err)
		}
		expMax, err = time.Parse("2006-01-02", f.ExpiryWindow.Max)
		if err != nil {
			return nil, fmt.Errorf("resolve: bad expiry max %q: %w", f.ExpiryWindow.Max, err)
		}
		if expMax.Before(expMin) {
			return nil, fmt.Errorf("resolve: expiry max %s before min %s", f.ExpiryWindow.Max, f.ExpiryWindow.Min)
		}
	}
	if f.StrikeWindow != nil && f.StrikeWindow.Max < f.StrikeWindow.Min {
		return nil, fmt.Errorf("resolve: strike max %g before min %g", f.StrikeWindow.Max, f.StrikeWindow.Min)
	}

	out := make([]api.OptionSymbol, 0, len(opts))
	for _, o := range opts {
		if !matchKind(o, f.Kind) {
			continue
		}
		if f.ExpiryWindow != nil {
			exp, err := time.Parse("2006-01-02", o.ExpirationDate)
			if err != nil {
				// Skip malformed rather than aborting — tastytrade
				// occasionally returns odd dates for delisted series.
				continue
			}
			if exp.Before(expMin) || exp.After(expMax) {
				continue
			}
		}
		if f.StrikeWindow != nil {
			strike, err := strconv.ParseFloat(o.StrikePrice, 64)
			if err != nil {
				continue
			}
			if strike < f.StrikeWindow.Min || strike > f.StrikeWindow.Max {
				continue
			}
		}
		if o.StreamerSymbol == "" {
			continue
		}
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ExpirationDate != out[j].ExpirationDate {
			return out[i].ExpirationDate < out[j].ExpirationDate
		}
		si, _ := strconv.ParseFloat(out[i].StrikePrice, 64)
		sj, _ := strconv.ParseFloat(out[j].StrikePrice, 64)
		if si != sj {
			return si < sj
		}
		return out[i].OptionType < out[j].OptionType
	})
	return out, nil
}

// matchKind returns true iff o.OptionType (tastytrade uses "C"/"P" in
// the `option` JSON field) matches the requested kind.
func matchKind(o api.OptionSymbol, kind string) bool {
	switch kind {
	case "", "both":
		return true
	case "call":
		return strings.EqualFold(o.OptionType, "C") || strings.EqualFold(o.OptionType, "call")
	case "put":
		return strings.EqualFold(o.OptionType, "P") || strings.EqualFold(o.OptionType, "put")
	}
	return false
}
