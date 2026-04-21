package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/brojonat/godxfeed/service"
	sapi "github.com/brojonat/godxfeed/service/api"
	"github.com/brojonat/godxfeed/service/llm"
)

// ProviderRegistry holds one LLM provider per canonical name
// ("anthropic", "openai", "gemini"). The handler picks a provider by
// request body (optional) or falls back to the registry's default.
// Empty registries disable the endpoint.
type ProviderRegistry struct {
	Providers map[string]llm.Provider
	Default   string
}

// nlSubscribeRequest is the wire shape the handler accepts.
type nlSubscribeRequest struct {
	Text     string `json:"text"`
	Provider string `json:"provider,omitempty"` // optional override
}

// nlSubscribeResponse mirrors the FilterSpec + the subs that actually
// landed so callers can see exactly what the LLM extracted and what
// the resolver produced. Useful for debugging / UX ("I asked for X,
// got Y subscriptions, does this look right?").
type nlSubscribeResponse struct {
	Provider string           `json:"provider"`
	Filter   llm.FilterSpec   `json:"filter"`
	Subs     []nlSubscription `json:"subs"`
}

type nlSubscription struct {
	Event   string `json:"event"`
	Symbol  string `json:"symbol"`
	Subject string `json:"subject"`
}

// handleNLSubscribe: POST /nl-subscribe {text, provider?} → extract
// FilterSpec via LLM, resolve to concrete subs against tastytrade
// symbology, and hand them to SubscriptionManager.BulkAdd.
func handleNLSubscribe(tts service.Service, reg ProviderRegistry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if len(reg.Providers) == 0 {
			writeJSONResponse(w, map[string]string{"error": "nl-subscribe not configured (no LLM providers registered)"}, http.StatusServiceUnavailable)
			return
		}
		var req nlSubscribeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeBadRequestError(w, fmt.Errorf("parse body: %w", err))
			return
		}
		if req.Text == "" {
			writeBadRequestError(w, fmt.Errorf("text is required"))
			return
		}
		providerName := req.Provider
		if providerName == "" {
			providerName = reg.Default
		}
		prov, ok := reg.Providers[providerName]
		if !ok {
			writeBadRequestError(w, fmt.Errorf("unknown provider %q (configured: %v)", providerName, registryNames(reg)))
			return
		}

		// Extract intent.
		filter, err := prov.Extract(r.Context(), req.Text, time.Now())
		if err != nil {
			tts.Log(int(slog.LevelWarn), "nl-subscribe: extract failed", "provider", prov.Name(), "err", err)
			writeJSONResponse(w, map[string]string{"error": fmt.Sprintf("extract: %s", err)}, http.StatusBadGateway)
			return
		}

		// Resolve against tastytrade symbology.
		resolver := &llm.Resolver{Source: &serviceSymbology{svc: tts}}
		subs, err := resolver.Resolve(filter)
		if err != nil {
			tts.Log(int(slog.LevelWarn), "nl-subscribe: resolve failed", "filter", filter, "err", err)
			writeJSONResponse(w, map[string]string{"error": fmt.Sprintf("resolve: %s", err)}, http.StatusBadRequest)
			return
		}

		// Dispatch in one wire call.
		pairs := make([]struct{ Event, Symbol string }, len(subs))
		for i, s := range subs {
			pairs[i] = struct{ Event, Symbol string }{Event: s.Event, Symbol: s.Symbol}
		}
		if err := tts.BulkAddSubscriptions(pairs); err != nil {
			tts.Log(int(slog.LevelError), "nl-subscribe: bulk add failed", "err", err)
			writeInternalError(tts, w, err)
			return
		}

		// Echo back what was applied. Subject is synthesized the same
		// way the ingress will publish it, so UIs can subscribe
		// immediately without re-polling /dxlink/subscriptions.
		out := nlSubscribeResponse{
			Provider: prov.Name(),
			Filter:   filter,
			Subs:     make([]nlSubscription, len(subs)),
		}
		for i, s := range subs {
			out.Subs[i] = nlSubscription{Event: s.Event, Symbol: s.Symbol, Subject: service.SubjectFor(s.Event, s.Symbol)}
		}
		writeJSONResponse(w, out, http.StatusOK)
	}
}

func registryNames(reg ProviderRegistry) []string {
	names := make([]string, 0, len(reg.Providers))
	for n := range reg.Providers {
		names = append(names, n)
	}
	return names
}

// serviceSymbology adapts a service.Service to llm.SymbologySource —
// the tiny interface the Resolver actually needs. Keeps the llm
// package ignorant of the full Service surface.
type serviceSymbology struct {
	svc service.Service
}

func (s *serviceSymbology) EquityStreamerSymbol(root string) (string, error) {
	resp, err := s.svc.GetSymbolData(root, sapi.SYMBOL_TYPE_EQUITIES)
	if err != nil {
		return "", err
	}
	var eq sapi.EquitySymbol
	if err := json.Unmarshal(resp.Data, &eq); err != nil {
		return "", fmt.Errorf("parse equity symbol: %w", err)
	}
	if eq.StreamerSymbol == "" {
		// Fall back to the equity symbol itself — most liquid equities
		// streamer-symbol-equals-ticker, and tastytrade sometimes
		// omits the streamer-symbol field for index proxies.
		return eq.Symbol, nil
	}
	return eq.StreamerSymbol, nil
}

func (s *serviceSymbology) OptionSymbols(root string) ([]sapi.OptionSymbol, error) {
	resp, err := s.svc.GetSymbolData(root, sapi.SYMBOL_TYPE_OPTIONS)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Items []sapi.OptionSymbol `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &payload); err != nil {
		return nil, fmt.Errorf("parse option chain: %w", err)
	}
	return payload.Items, nil
}
