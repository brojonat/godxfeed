package http

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/brojonat/godxfeed/service"
)

// handleNATSStream is a tiny lookup endpoint: given a symbol, return the NATS
// subject the browser should subscribe to. Kept as a redirection layer so the
// subject scheme can evolve (Phase 3 will split per event type) without
// touching every client.
func handleNATSStream(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		symbol := r.URL.Query().Get("symbol")
		if symbol == "" {
			writeBadRequestError(w, fmt.Errorf("missing symbol parameter"))
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(struct {
			Subject string `json:"subject"`
		}{Subject: "godxfeed." + symbol})
	}
}
