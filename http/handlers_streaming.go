package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/brojonat/godxfeed/service"
)

// handleNATSStream is a tiny lookup endpoint: given a symbol (and optionally
// an event type), return the NATS subject the browser should subscribe to.
// Subject scheme is `godxfeed.<event>.<symbol>` with the event lowercased.
// Defaults to `quote` for backward compatibility with callers that don't
// specify one.
func handleNATSStream(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		symbol := r.URL.Query().Get("symbol")
		if symbol == "" {
			writeBadRequestError(w, fmt.Errorf("missing symbol parameter"))
			return
		}
		event := strings.ToLower(r.URL.Query().Get("event"))
		if event == "" {
			event = "quote"
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(struct {
			Subject string `json:"subject"`
		}{Subject: "godxfeed." + event + "." + symbol})
	}
}
