package http

import (
	"encoding/json"
	"net/http"

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
