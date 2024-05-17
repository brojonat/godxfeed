package tastytrade

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	bwebsocket "github.com/brojonat/websocket"
	"github.com/gorilla/handlers"
	"github.com/gorilla/mux"
)

var upgrader = bwebsocket.DefaultUpgrader([]string{"http://localhost:9000", "http://localhost:9000"})

func writeJSONError(w http.ResponseWriter, err interface{}, code int) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(err)
}

func writeHTTPResponse(s Service, w http.ResponseWriter, twr *Response, err error) {
	if err != nil {
		var ise ErrorInternal
		var bre ErrorBadRequest
		if errors.As(err, &ise) {
			s.Log(int(slog.LevelError), fmt.Sprintf("internal error: %s", err.Error()))
			writeJSONError(w, ErrorInternal{Message: "internal error"}, http.StatusInternalServerError)
			return
		} else if errors.As(err, &bre) {
			writeJSONError(w, err, http.StatusBadRequest)
			return
		}
		s.Log(int(slog.LevelError), fmt.Sprintf("unhandled error: %s", err.Error()))
		writeJSONError(w, ErrorInternal{Message: err.Error()}, http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(twr)
}

func RunHTTPServer(
	tts Service,
	addr,
	twEndpoint,
	twToken,
	dxEndpoint,
	dxToken string,
) error {

	r := mux.NewRouter()
	r.Handle("/session-token", HandleNewSessionToken(tts, twEndpoint)).Methods(http.MethodGet)
	r.Handle("/test-session-token", HandleTestSessionToken(tts, twEndpoint)).Methods(http.MethodGet)
	r.Handle("/streamer-token", HandleNewStreamerToken(tts, twEndpoint)).Methods(http.MethodGet)
	r.Handle("/symbol", HandleGetSymbolData(tts, twEndpoint)).Methods(http.MethodGet)
	r.Handle("/option-chain", HandleGetOptionChain(tts, twEndpoint)).Methods(http.MethodGet)
	r.Handle("/ws", ServeWS(tts, twEndpoint, twToken, dxEndpoint, dxToken)).Methods(http.MethodGet)

	h := handlers.CORS(
		handlers.AllowedHeaders([]string{"X-Requested-With", "Content-Type", "Authorization"}),
		handlers.AllowedOrigins([]string{"*"}),
		handlers.AllowedMethods([]string{http.MethodGet, http.MethodPost, http.MethodOptions}),
	)(r)
	tts.Log(int(slog.LevelInfo), fmt.Sprintf("listening on %s", addr))
	return http.ListenAndServe(addr, h)
}

func HandleTestSessionToken(s Service, endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		st := r.URL.Query()["session-token"][0]
		twr, err := s.TestSessionToken(endpoint, st)
		writeHTTPResponse(s, w, twr, err)
	}
}

func HandleNewSessionToken(s Service, endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := r.URL.Query()["username"][0]
		p := r.URL.Query()["password"][0]
		twr, err := s.NewSessionToken(endpoint, u, p)
		writeHTTPResponse(s, w, twr, err)
	}
}

func HandleNewStreamerToken(s Service, endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionToken := r.URL.Query()["session-token"][0]
		twr, err := s.NewStreamerToken(endpoint, sessionToken)
		writeHTTPResponse(s, w, twr, err)
	}
}

func HandleGetSymbolData(s Service, endpoint string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionToken := r.URL.Query()["session-token"][0]
		symbol := r.URL.Query()["symbol"][0]
		symbolType := r.URL.Query()["symbolType"][0]
		twr, err := s.GetSymbolData(endpoint, sessionToken, symbol, symbolType)
		writeHTTPResponse(s, w, twr, err)
	}
}

func HandleGetOptionChain(s Service, endpoint string) http.HandlerFunc {
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
		writeHTTPResponse(s, w, twr, err)
	}
}

func ServeWS(tts Service, twEndpoint, twToken, dxEndpoint, dxToken string) http.HandlerFunc {
	return bwebsocket.ServeWS(
		upgrader,
		bwebsocket.DefaultSetupConn,
		bwebsocket.NewClient,
		tts.RegisterClient,
		tts.UnregisterClient,
		45*time.Second,
		tts.HandleClientMessage(twEndpoint, twToken, dxEndpoint, dxToken),
	)

}

func (s *service) NewSessionToken(endpoint, u, p string) (*Response, error) {
	url := fmt.Sprintf("https://%s/sessions", endpoint)
	bdata := struct {
		Login      string `json:"login"`
		Password   string `json:"password"`
		RememberMe bool   `json:"remember-me"`
	}{
		Login:      u,
		Password:   p,
		RememberMe: false,
	}
	b, err := json.Marshal(bdata)
	if err != nil {
		return nil, fmt.Errorf("%w: could not marshal request body: %w", ErrorBadRequest{Message: "bad request"}, err)
	}

	r, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", ErrorInternal{Message: "internal error"}, err)

	}

	addDefaultHeaders(r.Header)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", ErrorInternal{Message: "internal error"}, err)
	}

	b, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", ErrorInternal{Message: "internal error"}, err)
	}

	var response Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", ErrorInternal{Message: "internal error"}, err)
	}

	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated:
	default:
		return nil, fmt.Errorf("%w: unexpected response: %s\n%s", ErrorInternal{Message: "internal error"}, res.Status, b)
	}
	return &response, nil
}

func (s *service) TestSessionToken(endpoint, sessionToken string) (*Response, error) {
	url := fmt.Sprintf("https://%s/customers/me", endpoint)
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", ErrorInternal{Message: "internal error"}, err)
	}

	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", ErrorInternal{Message: "internal error"}, err)
	}

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", ErrorInternal{Message: "internal error"}, err)
	}

	var response Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", ErrorInternal{Message: "internal error"}, err)

	}
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s\n%s", ErrorBadRequest{Message: "bad auth credentials"}, res.Status, b)
	default:
		return nil, fmt.Errorf("%w: unexpected response: %s\n%s", ErrorInternal{Message: "internal error"}, res.Status, b)
	}
	return &response, nil
}

func (s *service) NewStreamerToken(endpoint, sessionToken string) (*Response, error) {
	url := fmt.Sprintf("https://%s/api-quote-tokens", endpoint)
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", ErrorInternal{Message: "internal error"}, err)
	}

	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", ErrorInternal{Message: "internal error"}, err)
	}

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", ErrorInternal{Message: "internal error"}, err)
	}

	var response Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", ErrorInternal{Message: "internal error"}, err)

	}
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s\n%s", ErrorBadRequest{Message: "bad auth credentials"}, res.Status, b)
	default:
		return nil, fmt.Errorf("%w: unexpected response: %s\n%s", ErrorInternal{Message: "internal error"}, res.Status, b)
	}
	return &response, nil
}

func (s *service) GetSymbolData(endpoint, sessionToken, symbol, symbolType string) (*Response, error) {
	sym := strings.ToUpper(symbol)
	symType := strings.ToLower(symbolType)
	url, err := GetSymbologyURL(endpoint, symType, sym)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrorBadRequest{Message: "bad request"}, err)
	}
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", ErrorInternal{Message: "internal error"}, err)
	}
	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", ErrorInternal{Message: "internal error"}, err)
	}

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", ErrorInternal{Message: "internal error"}, err)

	}

	var response Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", ErrorInternal{Message: "internal error"}, err)
	}
	return &response, nil
}

func (s *service) GetOptionChain(endpoint, sessionToken, symbol string, nested, compact bool) (*Response, error) {
	sym := strings.ToUpper(symbol)
	url := fmt.Sprintf("https://%s/option-chains/%s", endpoint, sym)
	if nested {
		url += "/nested"
	} else if compact {
		url += "/compact"
	}
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", ErrorInternal{Message: "internal error"}, err)
	}
	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", ErrorInternal{Message: "internal error"}, err)

	}

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", ErrorInternal{Message: "internal error"}, err)

	}

	var response Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", ErrorInternal{Message: "internal error"}, err)
	}
	return &response, nil
}
