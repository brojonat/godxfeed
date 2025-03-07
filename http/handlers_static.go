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

func setupStaticHandler() (http.Handler, error) {
	ridgelinePlotTemplate = template.Must(template.ParseFS(static, "static/templates/ridgeline.tmpl"))
	dynamicDistributionPlotTemplate = template.Must(template.ParseFS(static, "static/templates/dynamic_distribution.tmpl"))
	natsPlotTemplate = template.Must(template.ParseFS(static, "static/templates/nats.tmpl"))
	jsFS, err := fs.Sub(static, "static")
	if err != nil {
		return nil, fmt.Errorf("startup: failed to setup js static file server: %w", err)
	}
	return http.FileServer(http.FS(jsFS)), nil
}
