package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/brojonat/godxfeed/http/api"
	"github.com/brojonat/godxfeed/service"
	sapi "github.com/brojonat/godxfeed/service/api"
	"github.com/brojonat/server-tools/stools"
	"github.com/gorilla/websocket"
)

// FIXME: origins need updating
// actually not if we're not using websockets
// var upgrader = bwebsocket.DefaultUpgrader([]string{"http://localhost:9000", "http://localhost:9000"})
var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

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
	addr string,
	twEndpoint string,
	twToken string,
	dxEndpoint string,
	dxToken string,
	natsBrowserURL string,
) error {

	// new router
	mux := http.NewServeMux()

	// max body size, other parsing params
	maxBytes := int64(1048576)

	// subtle gotcha: if you omit this origin check, you'll get a CORS error
	// in the browser console and it's super annoying to debug.
	upgrader.CheckOrigin = func(r *http.Request) bool { return true }

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

	// setup static file server (this will also parse the templates that are embedded in the binary)
	staticHandler, err := setupStaticHandler()
	if err != nil {
		return fmt.Errorf("startup: failed to setup js static file server: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler))

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
	mux.Handle("POST /refresh-token", stools.AdaptHandler(
		handleRefreshToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("GET /test-bearer-token", stools.AdaptHandler(
		handleTestBearerToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("GET /test-session-token", stools.AdaptHandler(
		handleTestSessionToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("GET /nats-auth-callout", stools.AdaptHandler(
		handleNATSCallout(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
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
	mux.Handle("GET /streamer-token", stools.AdaptHandler(
		handleNewStreamerToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	// tastytrade data handlers
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

	// internal timeseries handlers
	mux.Handle("GET /timeseries/symbol-regexp", stools.AdaptHandler(
		handleGetSymbolRegexpTimeseriesData(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))

	// plots
	// example: http://localhost:8080/plots?plot_kind=line_chart&symbol=SPY
	mux.Handle("GET /plots", stools.AdaptHandler(
		handleGetPlots(tts, natsBrowserURL),
		atLeastOneAuth(basicAuthorizerCtxSetEmail(getSecretKey)),
	))

	addr = ":" + addr
	tts.Log(int(slog.LevelInfo), fmt.Sprintf("listening on %s", addr))
	return http.ListenAndServe(addr, mux)
}
