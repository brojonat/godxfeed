package http

import (
	"fmt"
	"net/http"

	"log/slog"

	"github.com/brojonat/godxfeed/service"
)

func handleNATSStream(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Get the symbol from query parameters
		symbol := r.URL.Query().Get("symbol")
		if symbol == "" {
			s.Log(int(slog.LevelError), "Missing symbol parameter")
			writeBadRequestError(w, fmt.Errorf("missing symbol parameter"))
			return
		}
		if symbol != "SPY" {
			writeBadRequestError(w, fmt.Errorf("symbol not supported by this server, supported symbols are [SPY]"))
			return
		}
		// clearing the stream isn't viable here, because we don't want to
		// clear the stream if this request is the result of a client
		// reconnecting to the service

		// if the stream has already been started, we don't need to do anything
		// so just return
		if s.IsSymbolStreamActive(symbol) {
			writeOK(w)
			return
		}	

		// Publish the historical data for this symbol (ideally this will
		// be like a stream upsert, so we don't need to worry about clearing
		// the stream)
		err := s.StreamHistoricalSymbolData(r.Context(), symbol)
		if err != nil {
			s.Log(int(slog.LevelError), "Failed to start symbol stream", "error", err.Error(), "symbol", symbol)
			writeInternalError(s, w, err)
			return
		}

		// make sure the service is streaming this symbol if its not already?
		// hmm, we typically control the services' symbol subscriptions when we
		// start it, so we may not need to do anything here, but in practice,
		// we need to make sure the service is streaming this symbol if its not
		// already, and we don't want to clear the stream here, because we don't
		// want to clear the stream if this request is the result of a client
		// reconnecting to the service

		writeOK(w)
	}
}
