package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/brojonat/godxfeed/http/api"
	"github.com/brojonat/godxfeed/service"
	sapi "github.com/brojonat/godxfeed/service/api"
	"github.com/brojonat/server-tools/stools"
	bwebsocket "github.com/brojonat/websocket"
)

// FIXME: origins need updating
var upgrader = bwebsocket.DefaultUpgrader([]string{"http://localhost:9000", "http://localhost:9000"})

func writeOK(w http.ResponseWriter) {
	resp := api.DefaultJSONResponse{Message: "ok"}
	writeJSONResponse(w, resp, http.StatusOK)
}

func writeInternalError(s service.Service, w http.ResponseWriter, e error) {
	s.Log(int(slog.LevelError), "internal error", "error", e.Error())
	resp := api.DefaultJSONResponse{Error: e.Error()}
	writeJSONResponse(w, resp, http.StatusInternalServerError)
}

func writeBadRequestError(w http.ResponseWriter, err error) {
	resp := api.DefaultJSONResponse{Error: err.Error()}
	writeJSONResponse(w, resp, http.StatusBadRequest)
}

func writeEmptyResultError(w http.ResponseWriter) {
	resp := api.DefaultJSONResponse{Error: "empty result set"}
	writeJSONResponse(w, resp, http.StatusNotFound)
}

func writeJSONResponse(w http.ResponseWriter, resp interface{}, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(resp)
}

func writeServiceResponse(s service.Service, w http.ResponseWriter, twr *sapi.Response, err error) {
	if err != nil {
		var ise sapi.ErrorInternal
		var bre sapi.ErrorBadRequest
		if errors.As(err, &ise) {
			s.Log(int(slog.LevelError), fmt.Sprintf("internal error: %s", err.Error()))
			writeJSONResponse(w, api.DefaultJSONResponse{Error: "internal error"}, http.StatusInternalServerError)
			return
		} else if errors.As(err, &bre) {
			writeJSONResponse(w, api.DefaultJSONResponse{Error: err.Error()}, http.StatusBadRequest)
			return
		}
		s.Log(int(slog.LevelError), fmt.Sprintf("unhandled error: %s", err.Error()))
		writeJSONResponse(w, api.DefaultJSONResponse{Error: err.Error()}, http.StatusInternalServerError)
		return
	}
	writeJSONResponse(w, twr, http.StatusOK)
}

func RunHTTPServer(
	ctx context.Context,
	tts service.Service,
	addr,
	twEndpoint,
	twToken,
	dxEndpoint,
	dxToken string,
) error {

	// new router
	mux := http.NewServeMux()

	// max body size, other parsing params
	maxBytes := int64(1048576)

	// parse and transform the comma separated envs that configure CORS
	hs := os.Getenv("CORS_HEADERS")
	ms := os.Getenv("CORS_METHODS")
	ogs := os.Getenv("CORS_ORIGINS")
	normalizeCORSParams := func(e string) []string {
		params := strings.Split(e, ",")
		for i, p := range params {
			params[i] = strings.ReplaceAll(p, " ", "")
		}
		return params
	}
	headers := normalizeCORSParams(hs)
	methods := normalizeCORSParams(ms)
	origins := normalizeCORSParams(ogs)

	// FIXME: once we're done iterating, shove this back into a embedded FS
	// ridgelinePlotTemplate = template.Must(template.ParseFS(static, "static/templates/plots/ridgeline.tmpl"))
	// jsFS, err := fs.Sub(static, "static")
	// if err != nil {
	// 	return nil, fmt.Errorf("startup: failed to setup js static file server: %w", err)
	// }
	// fsJS := http.FileServer(http.FS(jsFS))

	// note the peculiar pathing because the cli is run from the root directory
	ridgelinePlotTemplate = template.Must(template.ParseFiles("http/static/templates/plots/ridgeline.tmpl"))
	fsJS := http.FileServer(http.Dir("http/static"))
	mux.Handle("GET /static/", http.StripPrefix("/static/", fsJS))

	// smoke test/boot handlers
	mux.Handle("GET /ping", stools.AdaptHandler(
		stools.HandlePing(),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	// admin handlers
	// returns a Bearer token; basic auth protected
	mux.Handle("POST /token", stools.AdaptHandler(
		handleIssueToken(tts),
		atLeastOneAuth(basicAuthorizerCtxSetEmail(getSecretKey)),
	))

	// returns TastyTrade session token; requires Bearer token
	mux.Handle("GET /session-token", stools.AdaptHandler(
		handleNewSessionToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))
	// returns DXFeed streamer token; requires Bearer token
	mux.Handle("GET /test-session-token", stools.AdaptHandler(
		handleTestSessionToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	mux.Handle("GET /streamer-token", stools.AdaptHandler(
		handleNewStreamerToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	// data handlers
	mux.Handle("GET /symbol", stools.AdaptHandler(
		handleGetSymbolData(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))
	mux.Handle("GET /option-chain", stools.AdaptHandler(
		handleGetOptionChain(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))
	mux.Handle("GET /ws", stools.AdaptHandler(
		handleServeWS(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	// plots!
	mux.Handle("GET /plots", stools.AdaptHandler(
		handleGetPlots(tts),
		atLeastOneAuth(basicAuthorizerCtxSetEmail(getSecretKey)),
	))
	mux.Handle("GET /plot-dummy-data", stools.AdaptHandler(
		handleGetPlotDummyData(tts),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))

	addr = ":" + addr
	tts.Log(int(slog.LevelInfo), fmt.Sprintf("listening on %s", addr))
	return http.ListenAndServe(addr, mux)
}

func handleServeWS(tts service.Service) http.HandlerFunc {
	return bwebsocket.ServeWS(
		upgrader,
		bwebsocket.DefaultSetupConn,
		bwebsocket.NewClient,
		tts.WSClientRegister,
		tts.WSClientUnregister,
		45*time.Second,
		[]bwebsocket.MessageHandler{tts.WSClientHandleMessage()},
	)

}
