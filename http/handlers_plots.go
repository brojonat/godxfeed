package http

import (
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"

	"github.com/brojonat/godxfeed/service"
)

const (
	PlotKindRidgeLine           string = "ridgeline"
	PlotKindDynamicDistribution string = "dynamic_distribution"
	PlotKindNats                string = "nats"
	PlotKindLineChart           string = "line_chart"
	PlotKindOptionsGrid         string = "options_grid"
)

var ridgelinePlotTemplate *template.Template
var natsPlotTemplate *template.Template
var dynamicDistributionPlotTemplate *template.Template
var lineChartPlotTemplate *template.Template
var optionsGridTemplate *template.Template

type ridgelinePlotTemplateData struct {
	Endpoint                 string
	PlotKind                 string
	LocalStorageAuthTokenKey string
	Symbol                   string
}
type dynamicDistributionPlotTemplateData struct {
	Endpoint                 string
	NATSURL                  string
	PlotKind                 string
	LocalStorageAuthTokenKey string
	Symbol                   string
	BasicAuthEmail           string
	BasicAuthPassword        string
}
type natsPlotTemplateData struct {
	Endpoint                 string
	NATSURL                  string
	PlotKind                 string
	LocalStorageAuthTokenKey string
	Symbol                   string
	BasicAuthEmail           string
	BasicAuthPassword        string
}
type lineChartPlotTemplateData struct {
	Endpoint                 string
	NATSURL                  string
	PlotKind                 string
	LocalStorageAuthTokenKey string
	Symbol                   string
	BasicAuthEmail           string
	BasicAuthPassword        string
}
type optionsGridTemplateData struct {
	Endpoint                 string
	PlotKind                 string
	LocalStorageAuthTokenKey string
	Symbol                   string
	BasicAuthEmail           string
	BasicAuthPassword        string
}

type indexTemplateData struct {
	Endpoint                 string
	LocalStorageAuthTokenKey string
	BasicAuthEmail           string
	BasicAuthPassword        string
}

func handleGetPlots(s service.Service, natsBrowserURL string) http.HandlerFunc {
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
		case PlotKindDynamicDistribution:
			data := dynamicDistributionPlotTemplateData{
				Endpoint:                 os.Getenv("GODXFEED_ENDPOINT"),
				NATSURL:                  natsBrowserURL,
				LocalStorageAuthTokenKey: os.Getenv("LOCAL_STORAGE_AUTH_TOKEN_KEY"),
				PlotKind:                 pk,
				Symbol:                   symbol,
			}
			w.WriteHeader(http.StatusOK)
			err := dynamicDistributionPlotTemplate.Execute(w, data)
			if err != nil {
				s.Log(int(slog.LevelError), "Error rendering template", "error", err)
				writeInternalError(s, w, err)
				return
			}
		case PlotKindNats:
			data := natsPlotTemplateData{
				Endpoint:                 os.Getenv("GODXFEED_ENDPOINT"),
				NATSURL:                  natsBrowserURL,
				BasicAuthEmail:           "brojonat@gmail.com",
				BasicAuthPassword:        os.Getenv("SECRET_KEY"),
				LocalStorageAuthTokenKey: os.Getenv("LOCAL_STORAGE_AUTH_TOKEN_KEY"),
				PlotKind:                 pk,
				Symbol:                   symbol,
			}
			w.WriteHeader(http.StatusOK)
			err := natsPlotTemplate.Execute(w, data)
			if err != nil {
				s.Log(int(slog.LevelError), "Error rendering template", "error", err)
				writeInternalError(s, w, err)
				return
			}
		case PlotKindLineChart:
			data := lineChartPlotTemplateData{
				Endpoint:                 os.Getenv("GODXFEED_ENDPOINT"),
				NATSURL:                  natsBrowserURL,
				LocalStorageAuthTokenKey: os.Getenv("LOCAL_STORAGE_AUTH_TOKEN_KEY"),
				PlotKind:                 pk,
				Symbol:                   symbol,
				BasicAuthEmail:           "brojonat@gmail.com",
				BasicAuthPassword:        os.Getenv("SECRET_KEY"),
			}
			w.WriteHeader(http.StatusOK)
			err := lineChartPlotTemplate.Execute(w, data)
			if err != nil {
				s.Log(int(slog.LevelError), "Error rendering template", "error", err)
				writeInternalError(s, w, err)
				return
			}
		case PlotKindOptionsGrid:
			data := optionsGridTemplateData{
				Endpoint:                 os.Getenv("GODXFEED_ENDPOINT"),
				LocalStorageAuthTokenKey: os.Getenv("LOCAL_STORAGE_AUTH_TOKEN_KEY"),
				PlotKind:                 pk,
				Symbol:                   symbol,
				BasicAuthEmail:           "brojonat@gmail.com",
				BasicAuthPassword:        os.Getenv("SECRET_KEY"),
			}
			w.WriteHeader(http.StatusOK)
			err := optionsGridTemplate.Execute(w, data)
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

func handleIndex() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := indexTemplateData{
			Endpoint:                 os.Getenv("GODXFEED_ENDPOINT"),
			LocalStorageAuthTokenKey: os.Getenv("LOCAL_STORAGE_AUTH_TOKEN_KEY"),
			BasicAuthEmail:           "brojonat@gmail.com",
			BasicAuthPassword:        os.Getenv("SECRET_KEY"),
		}

		w.WriteHeader(http.StatusOK)
		err := indexTemplate.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}
