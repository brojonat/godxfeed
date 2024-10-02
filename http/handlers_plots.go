package http

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"

	"github.com/brojonat/godxfeed/service"
)

const (
	PlotKindRidgeLine string = "ridgeline"
)

var ridgelinePlotTemplate *template.Template

type ridgelinePlotTemplateData struct {
	Endpoint                 string
	PlotKind                 string
	LocalStorageAuthTokenKey string
	Symbol                   string
}

func handleGetPlots(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pk := r.URL.Query().Get("plot_kind")
		symbol := r.URL.Query().Get("symbol")
		if symbol == "" {
			writeBadRequestError(w, fmt.Errorf("must supply symbol"))
			return
		}
		switch pk {
		// pretty much all plots should be served by this same template
		case PlotKindRidgeLine:
			data := ridgelinePlotTemplateData{
				Endpoint:                 os.Getenv("GODXFEED_ENDPOINT"),
				LocalStorageAuthTokenKey: os.Getenv("LOCAL_STORAGE_AUTH_TOKEN_KEY"),
				PlotKind:                 pk,
				Symbol:                   symbol,
			}
			w.WriteHeader(http.StatusOK)
			err := ridgelinePlotTemplate.Execute(w, data)
			if err != nil {
				s.Log(int(slog.LevelError), "Error rendering template", "error", err)
				writeInternalError(s, w, err)
				return
			}
		default:
			writeBadRequestError(w, fmt.Errorf("unsupported plot_kind %s", pk))
			return
		}

	}
}

func handleGetPlotDummyData(s service.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := []struct {
			X float64
			Y float64
		}{
			{123, 123},
			{125, 128},
			{156, 186},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(data)
	}
}
