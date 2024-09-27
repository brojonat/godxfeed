package http

import (
	"net/http"

	"github.com/brojonat/godxfeed/service"
)

func handleGetSymbolData(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		symbol := r.URL.Query()["symbol"][0]
		symbolType := r.URL.Query()["symbolType"][0]
		twr, err := s.GetSymbolData(symbol, symbolType)
		writeServiceResponse(s, w, twr, err)
	}
}

func handleGetOptionChain(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		symbol := r.URL.Query()["symbol"][0]
		twr, err := s.GetOptionChain(symbol)
		writeServiceResponse(s, w, twr, err)
	}
}
