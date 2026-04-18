package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/brojonat/godxfeed/http/api"
	"github.com/brojonat/godxfeed/service"
)

// handleDXLinkStatus returns the state of the service's dxLink WebSocket.
func handleDXLinkStatus(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(s.DXLinkStatus())
	}
}

// handleDXLinkSubscriptions returns a snapshot of every active (event, symbol)
// subscription, its NATS subject, and traffic counters.
func handleDXLinkSubscriptions(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := s.Subscriptions()
		if snap == nil {
			snap = []service.SubscriptionInfo{}
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(snap)
	}
}

type subscriptionMutationRequest struct {
	Event  string `json:"event"`
	Symbol string `json:"symbol"`
}

// parseMutation decodes + validates the add/remove body. "Quote" is the
// default event type so the simplest client (POST /dxlink/subscriptions
// with just {"symbol":"SPY"}) works without spelling out the event field.
func parseMutation(r *http.Request) (event, symbol string, err error) {
	var body subscriptionMutationRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return "", "", fmt.Errorf("invalid JSON body: %w", err)
	}
	body.Symbol = strings.TrimSpace(body.Symbol)
	body.Event = strings.TrimSpace(body.Event)
	if body.Symbol == "" {
		return "", "", fmt.Errorf("symbol is required")
	}
	if body.Event == "" {
		body.Event = "Quote"
	}
	return body.Event, body.Symbol, nil
}

// handleAddSubscription handles POST /dxlink/subscriptions.
// Body: {"event":"Quote","symbol":"SPY"} — event defaults to "Quote".
// Idempotent: re-adding is a 200 no-op.
func handleAddSubscription(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, symbol, err := parseMutation(r)
		if err != nil {
			writeBadRequestError(w, err)
			return
		}
		if err := s.AddSubscription(event, symbol); err != nil {
			writeInternalError(s, w, err)
			return
		}
		writeJSONResponse(w, api.DefaultJSONResponse{Message: "subscribed"}, http.StatusOK)
	}
}

// handleRemoveSubscription handles DELETE /dxlink/subscriptions.
// Same body shape as the add handler. Removing a non-existent
// subscription is a 200 no-op.
func handleRemoveSubscription(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event, symbol, err := parseMutation(r)
		if err != nil {
			writeBadRequestError(w, err)
			return
		}
		if err := s.RemoveSubscription(event, symbol); err != nil {
			writeInternalError(s, w, err)
			return
		}
		writeJSONResponse(w, api.DefaultJSONResponse{Message: "unsubscribed"}, http.StatusOK)
	}
}
