package http

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
)

//go:embed static
var static embed.FS

// Add at the top with other template variables
var indexTemplate *template.Template

func setupStaticHandler() (http.Handler, error) {
	devMode := true
	if devMode {
		// In development mode, load templates from disk
		indexTemplate = template.Must(template.ParseFiles("http/static/templates/index.tmpl"))
		ridgelinePlotTemplate = template.Must(template.ParseFiles("http/static/templates/ridgeline.tmpl"))
		dynamicDistributionPlotTemplate = template.Must(template.ParseFiles("http/static/templates/dynamic_distribution.tmpl"))
		natsPlotTemplate = template.Must(template.ParseFiles("http/static/templates/nats.tmpl"))
		lineChartPlotTemplate = template.Must(template.ParseFiles("http/static/templates/line_chart.tmpl"))
		optionsGridTemplate = template.Must(template.ParseFiles("http/static/templates/options_grid.tmpl"))

		// Serve static files directly from disk
		return http.FileServer(http.Dir("http/static")), nil
	}

	// In production mode, use embedded files
	indexTemplate = template.Must(template.ParseFS(static, "static/templates/index.tmpl"))
	ridgelinePlotTemplate = template.Must(template.ParseFS(static, "static/templates/ridgeline.tmpl"))
	dynamicDistributionPlotTemplate = template.Must(template.ParseFS(static, "static/templates/dynamic_distribution.tmpl"))
	natsPlotTemplate = template.Must(template.ParseFS(static, "static/templates/nats.tmpl"))
	lineChartPlotTemplate = template.Must(template.ParseFS(static, "static/templates/line_chart.tmpl"))
	optionsGridTemplate = template.Must(template.ParseFS(static, "static/templates/options_grid.tmpl"))

	jsFS, err := fs.Sub(static, "static")
	if err != nil {
		return nil, fmt.Errorf("startup: failed to setup js static file server: %w", err)
	}
	return http.FileServer(http.FS(jsFS)), nil
}
