package http

import (
	"net/http"

	"github.com/brojonat/godxfeed/service"
)

func handleGetSymbolData(s service.Service, endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionToken := r.URL.Query()["session-token"][0]
		symbol := r.URL.Query()["symbol"][0]
		symbolType := r.URL.Query()["symbolType"][0]
		twr, err := s.GetSymbolData(endpoint, sessionToken, symbol, symbolType)
		writeServiceResponse(s, w, twr, err)
	}
}

func handleGetOptionChain(s service.Service, endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionToken := r.URL.Query()["session-token"][0]
		symbol := r.URL.Query()["symbol"][0]
		var nested, compact bool
		if _, ok := r.URL.Query()["nested"]; ok {
			nested = true
		}
		if _, ok := r.URL.Query()["compact"]; ok {
			compact = true
		}
		twr, err := s.GetOptionChain(endpoint, sessionToken, symbol, nested, compact)
		writeServiceResponse(s, w, twr, err)
	}
}
