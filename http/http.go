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
)

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

func writeJSONResponse(w http.ResponseWriter, resp any, code int) {
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
	dxEndpoint string,
	natsBrowserURL string,
	devMode bool,
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

	// setup static file server (this will also parse the templates that are embedded in the binary)
	staticHandler, err := setupStaticHandler(devMode)
	if err != nil {
		return fmt.Errorf("startup: failed to setup js static file server: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticHandler))

	// smoke test/boot handlers
	mux.Handle("GET /ping", adaptHandler(
		handlePing(),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	// admin handlers
	// returns a Bearer token; basic auth protected
	mux.Handle("POST /token", adaptHandler(
		handleIssueToken(tts),
		atLeastOneAuth(basicAuthorizerCtxSetEmail(getSecretKey)),
	))
	mux.Handle("POST /refresh-token", adaptHandler(
		handleRefreshToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("GET /test-bearer-token", adaptHandler(
		handleTestBearerToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("GET /nats-auth-callout", adaptHandler(
		handleNATSCallout(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))

	// returns DXFeed streamer token; requires Bearer token
	mux.Handle("GET /streamer-token", adaptHandler(
		handleNewStreamerToken(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	// tastytrade data handlers
	mux.Handle("GET /symbol", adaptHandler(
		handleGetSymbolData(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))
	mux.Handle("GET /option-chain", adaptHandler(
		handleGetOptionChain(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(
			bearerAuthorizerCtxSetToken(getSecretKey),
		),
	))

	// NATS streaming endpoint
	mux.Handle("POST /stream", adaptHandler(
		handleNATSStream(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))

	// dxLink admin/observability endpoints
	mux.Handle("GET /dxlink/status", adaptHandler(
		handleDXLinkStatus(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("GET /dxlink/subscriptions", adaptHandler(
		handleDXLinkSubscriptions(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("POST /dxlink/subscriptions", adaptHandler(
		handleAddSubscription(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))
	mux.Handle("DELETE /dxlink/subscriptions", adaptHandler(
		handleRemoveSubscription(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		atLeastOneAuth(bearerAuthorizerCtxSetToken(getSecretKey)),
	))

	// plots + admin: the HTML shell itself is public (no secrets embedded
	// — just the NATS browser URL and the server endpoint string). The
	// page's JS handles auth: it accepts ?token=... in the URL on first
	// visit, stashes it in localStorage, and shows a login modal if no
	// token is available. Guarding the HTML server-side blocked that
	// flow because the browser can't send an Authorization header on a
	// plain navigation — users landed on a 401 before any JS ran. All
	// the *data* endpoints the page consumes (/dxlink/*, /stream, etc.)
	// remain bearer-gated, so no content is actually exposed.
	mux.Handle("GET /plots", adaptHandler(
		handleGetPlots(tts, natsBrowserURL),
	))
	mux.Handle("GET /admin", adaptHandler(
		handleAdmin(tts, natsBrowserURL),
	))

	// webhook handlers
	mux.Handle("POST /webhook/buy-me-a-coffee", adaptHandler(
		handleBMCWebhook(tts),
		apiMode(tts, maxBytes, headers, methods, origins),
		bmcWebhookAuthorizer(tts, getWebhookSecret),
	))

	// Add this near the other route handlers in RunHTTPServer
	mux.Handle("GET /", adaptHandler(
		handleIndex(),
	))

	addr = ":" + addr
	tts.Log(int(slog.LevelInfo), fmt.Sprintf("listening on %s", addr))
	return http.ListenAndServe(addr, mux)
}
