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
	PlotKindDynamicDistribution string = "dynamic_distribution"
	PlotKindNats                string = "nats"
	PlotKindLineChart           string = "line_chart"
	PlotKindOptionsGrid         string = "options_grid"
	PlotKindSymbolDetail        string = "symbol_detail"
)

var natsPlotTemplate *template.Template
var dynamicDistributionPlotTemplate *template.Template
var lineChartPlotTemplate *template.Template
var optionsGridTemplate *template.Template
var symbolDetailTemplate *template.Template

type dynamicDistributionPlotTemplateData struct {
	Endpoint string
	NATSURL  string
	PlotKind string
	Symbol   string
}

type natsPlotTemplateData struct {
	Endpoint string
	NATSURL  string
	PlotKind string
	Symbol   string
}

type lineChartPlotTemplateData struct {
	Endpoint string
	NATSURL  string
	PlotKind string
	Symbol   string
}

type optionsGridTemplateData struct {
	Endpoint string
	PlotKind string
	Symbol   string
}

type symbolDetailTemplateData struct {
	Endpoint string
	NATSURL  string
	PlotKind string
	Symbol   string
}

type indexTemplateData struct {
	Endpoint string
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
		case PlotKindDynamicDistribution:
			data := dynamicDistributionPlotTemplateData{
				Endpoint: os.Getenv("GODXFEED_ENDPOINT"),
				NATSURL:  natsBrowserURL,
				PlotKind: pk,
				Symbol:   symbol,
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
				Endpoint: os.Getenv("GODXFEED_ENDPOINT"),
				NATSURL:  natsBrowserURL,
				PlotKind: pk,
				Symbol:   symbol,
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
				Endpoint: os.Getenv("GODXFEED_ENDPOINT"),
				NATSURL:  natsBrowserURL,
				PlotKind: pk,
				Symbol:   symbol,
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
				Endpoint: os.Getenv("GODXFEED_ENDPOINT"),
				PlotKind: pk,
				Symbol:   symbol,
			}
			w.WriteHeader(http.StatusOK)
			err := optionsGridTemplate.Execute(w, data)
			if err != nil {
				s.Log(int(slog.LevelError), "Error rendering template", "error", err)
				writeInternalError(s, w, err)
				return
			}
		case PlotKindSymbolDetail:
			data := symbolDetailTemplateData{
				Endpoint: os.Getenv("GODXFEED_ENDPOINT"),
				NATSURL:  natsBrowserURL,
				PlotKind: pk,
				Symbol:   symbol,
			}
			w.WriteHeader(http.StatusOK)
			err := symbolDetailTemplate.Execute(w, data)
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
			Endpoint: os.Getenv("GODXFEED_ENDPOINT"),
		}

		w.WriteHeader(http.StatusOK)
		err := indexTemplate.Execute(w, data)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

type adminTemplateData struct {
	Endpoint string
	NATSURL  string
}

func handleAdmin(s service.Service, natsBrowserURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := adminTemplateData{
			Endpoint: os.Getenv("GODXFEED_ENDPOINT"),
			NATSURL:  natsBrowserURL,
		}
		w.WriteHeader(http.StatusOK)
		if err := adminTemplate.Execute(w, data); err != nil {
			s.Log(int(slog.LevelError), "admin template render", "err", err)
			writeInternalError(s, w, err)
			return
		}
	}
}
