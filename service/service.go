package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	dx "github.com/brojonat/godxfeed/dxclient"
	"github.com/brojonat/godxfeed/service/api"
	"github.com/nats-io/nats.go"
)

func addDefaultHeaders(h http.Header) {
	h.Add("User-Agent", "tastytrade-api-client/1.0")
	h.Add("Content-Type", "application/json")
	h.Add("Accept", "application/json")
}

func getSymbologyURL(endpoint, symType, sym string) (string, error) {
	base := fmt.Sprintf("https://%s", endpoint)
	switch symType {
	case api.SYMBOL_TYPE_CRYPTO:
		base += "/instruments/cryptocurrencies"
	case api.SYMBOL_TYPE_FUTURES:
		base += "/instruments/futures"
	case api.SYMBOL_TYPE_EQUITIES:
		base += fmt.Sprintf("/instruments/equities/%s", sym)
	case api.SYMBOL_TYPE_OPTIONS:
		base += fmt.Sprintf("/option-chains/%s", sym)
	case api.SYMBOL_TYPE_FUTURES_OPTIONS:
		base += fmt.Sprintf("/futures-option-chains/%s", sym)
	default:
		return "", fmt.Errorf("unrecognized symbol type %s", symType)
	}
	return base, nil
}

// Service is the business-logic surface the HTTP and CLI layers consume.
// It deliberately stays small: tastytrade REST + the dxLink-to-NATS ingress
// path. Persistence lives in `service/analytics` sinks that subscribe to
// NATS independently.
type Service interface {
	Log(level int, m string, args ...any)
	NATS() *nats.Conn

	// Tastytrade REST.
	NewStreamerToken(ctx context.Context) (api.TokenData, error)
	GetSymbolData(symbol, symbolType string) (*api.Response, error)
	GetOptionChain(symbol string) (*api.Response, error)
	GetRelatedOptionSymbols(symbol string) ([]string, error)
	GetStreamSymbols(symbol string, method func(string) ([]string, error)) ([]string, error)

	// StartIngress dials dxLink, authenticates, subscribes to the initial
	// symbols, and begins publishing FEED_DATA events to NATS. Safe to call
	// once per service instance. Registers each symbol in the
	// SubscriptionManager.
	StartIngress(ctx context.Context, symbols []string) error

	// Subscriptions returns the current dxLink subscription state snapshot.
	Subscriptions() []SubscriptionInfo
	// DXLinkStatus returns the connection state of the dxLink WebSocket.
	DXLinkStatus() DXLinkStatus
}

// SymbolMethodNRelatedOptions returns a callback that fetches the first n
// related option symbols. Call with 0 to stream a single symbol only. See
// GetRelatedOptionSymbols for the tastytrade-side ordering (sorted by
// expiry then strike).
func SymbolMethodNRelatedOptions(s Service, n int) func(string) ([]string, error) {
	return func(symbol string) ([]string, error) {
		if n < 1 {
			return nil, nil
		}
		syms, err := s.GetRelatedOptionSymbols(symbol)
		if err != nil {
			return nil, err
		}
		if len(syms) < n {
			return syms, nil
		}
		return syms[:n], nil
	}
}

type service struct {
	twEndpoint string
	dxEndpoint string
	tp         TokenProvider
	logger     *slog.Logger
	nats       *nats.Conn
	subs       *SubscriptionManager

	// dxLink connection state. Everything inside here is guarded by dxMu.
	dxMu      sync.RWMutex
	dxClient  dx.Client
	dxStatus  DXLinkStatus
}

// NewService constructs the service. Note the absence of a *pgxpool / DB
// handle — persistence is delegated entirely to analytics sinks that have
// their own connections.
func NewService(
	twEndpoint string,
	dxEndpoint string,
	tp TokenProvider,
	l *slog.Logger,
	nc *nats.Conn,
) Service {
	return &service{
		twEndpoint: twEndpoint,
		dxEndpoint: dxEndpoint,
		tp:         tp,
		logger:     l,
		nats:       nc,
		subs:       NewSubscriptionManager(),
	}
}

// bearerAuth returns "Bearer <access_token>" for tastytrade API calls.
func (s *service) bearerAuth(ctx context.Context) (string, error) {
	if s.tp == nil {
		return "", fmt.Errorf("oauth: service has no TokenProvider configured")
	}
	tok, err := s.tp.AccessToken(ctx)
	if err != nil {
		return "", err
	}
	return "Bearer " + tok, nil
}

func (s *service) Log(level int, msg string, args ...any) {
	switch level {
	case int(slog.LevelDebug):
		s.logger.Debug(msg, args...)
	case int(slog.LevelInfo):
		s.logger.Info(msg, args...)
	case int(slog.LevelWarn):
		s.logger.Warn(msg, args...)
	case int(slog.LevelError):
		s.logger.Error(msg, args...)
	}
}

func (s *service) NATS() *nats.Conn { return s.nats }

func (s *service) Subscriptions() []SubscriptionInfo { return s.subs.Snapshot() }

func (s *service) DXLinkStatus() DXLinkStatus {
	s.dxMu.RLock()
	defer s.dxMu.RUnlock()
	return s.dxStatus
}

func (s *service) NewStreamerToken(ctx context.Context) (api.TokenData, error) {
	return fetchStreamerToken(ctx, http.DefaultClient, "https://"+s.twEndpoint, s.tp)
}

// GetSymbolData returns the symbol data for the supplied symbol and symbol type.
// Valid symbol types are "crypto", "futures", "equities", "options", "futures-options".
func (s *service) GetSymbolData(symbol, symbolType string) (*api.Response, error) {
	sym := strings.ToUpper(symbol)
	symType := strings.ToLower(symbolType)
	url, err := getSymbologyURL(s.twEndpoint, symType, sym)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorBadRequest{Message: "bad request"}, err)
	}
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	addDefaultHeaders(r.Header)
	auth, err := s.bearerAuth(r.Context())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	r.Header.Set("Authorization", auth)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	var response api.Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	if response.Error.Code != "" {
		return nil, fmt.Errorf("tastytrade error for symbology query: %s", response.Error.Message)
	}
	return &response, nil
}

func (s *service) GetOptionChain(symbol string) (*api.Response, error) {
	sym := strings.ToUpper(symbol)
	url := fmt.Sprintf("https://%s/option-chains/%s/compact", s.twEndpoint, sym)
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	addDefaultHeaders(r.Header)
	auth, err := s.bearerAuth(r.Context())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	r.Header.Set("Authorization", auth)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	defer res.Body.Close()

	b, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	var response api.Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	if response.Error.Code != "" {
		return nil, fmt.Errorf("tastytrade error for options-chain query: %s", response.Error.Message)
	}
	return &response, nil
}

// GetRelatedOptionSymbols returns the options streamer symbols for the
// supplied equity symbol, sorted by (daysToExpiration, strikePrice).
func (s *service) GetRelatedOptionSymbols(symbol string) ([]string, error) {
	twr, err := s.GetSymbolData(symbol, api.SYMBOL_TYPE_OPTIONS)
	if err != nil {
		return []string{}, fmt.Errorf("could not get option symbology data: %w", err)
	}
	var optsData struct {
		Items []api.OptionSymbol `json:"items"`
	}
	if err := json.Unmarshal(twr.Data, &optsData); err != nil {
		return []string{}, fmt.Errorf("could not unmarshal option symbol data: %w", err)
	}
	sort.Slice(optsData.Items, func(i, j int) bool {
		if optsData.Items[i].DaysToExpiration == optsData.Items[j].DaysToExpiration {
			ival, err := strconv.ParseFloat(optsData.Items[i].StrikePrice, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not parse strike: %v", err)
				return false
			}
			jval, err := strconv.ParseFloat(optsData.Items[j].StrikePrice, 64)
			if err != nil {
				fmt.Fprintf(os.Stderr, "could not parse strike: %v", err)
				return false
			}
			return ival < jval
		}
		return optsData.Items[i].DaysToExpiration < optsData.Items[j].DaysToExpiration
	})
	syms := []string{}
	for _, item := range optsData.Items {
		syms = append(syms, item.StreamerSymbol)
	}
	return syms, nil
}

func (s *service) GetStreamSymbols(symbol string, getRelateds func(string) ([]string, error)) ([]string, error) {
	twr, err := s.GetSymbolData(symbol, api.SYMBOL_TYPE_EQUITIES)
	if err != nil {
		return []string{}, fmt.Errorf("could not get equity symbol data: %w", err)
	}
	var eqData api.EquitySymbol
	if err := json.Unmarshal(twr.Data, &eqData); err != nil {
		return []string{}, fmt.Errorf("could not unmarshal equity symbol data: %w", err)
	}
	syms := []string{eqData.StreamerSymbol}

	relateds, err := getRelateds(symbol)
	if err != nil {
		return nil, fmt.Errorf("could not get related stream symbols: %w", err)
	}
	syms = append(syms, relateds...)
	return syms, nil
}

// StartIngress dials dxLink using a fresh streamer token, authenticates,
// subscribes to the supplied symbols, and starts a goroutine that publishes
// every incoming FEED_DATA event to NATS verbatim. Returns after the
// connection is established; the loop runs until ctx is canceled.
//
// This is the ONLY path from dxLink to NATS. Downstream (DB persister, UI,
// future Bayesian sidecar) are all NATS subscribers.
func (s *service) StartIngress(ctx context.Context, symbols []string) error {
	if s.nats == nil {
		return fmt.Errorf("ingress: service has no NATS connection")
	}

	s.dxMu.Lock()
	if s.dxClient != nil {
		s.dxMu.Unlock()
		return fmt.Errorf("ingress: already started")
	}

	cl := func(level int, msg string, args ...any) { s.Log(level, msg, args...) }
	s.dxClient = dx.NewClient(cl)

	td, err := s.NewStreamerToken(ctx)
	if err != nil {
		s.dxClient = nil
		s.dxMu.Unlock()
		return fmt.Errorf("ingress: get streamer token: %w", err)
	}
	dialURL := td.DXLinkURL
	if dialURL == "" {
		dialURL = "wss://" + s.dxEndpoint
	}
	s.Log(int(slog.LevelInfo), "dialing dxlink", "url", dialURL, "symbols", symbols)

	if err := s.dxClient.Dial(ctx, dialURL, func(msg dx.MessageSetup) error {
		s.Log(int(slog.LevelDebug), "dxlink setup reply", "msg", fmt.Sprintf("%+v", msg))
		return nil
	}); err != nil {
		s.dxClient = nil
		s.dxMu.Unlock()
		return fmt.Errorf("ingress: dial dxlink: %w", err)
	}
	s.dxStatus.Connected = true
	s.dxStatus.DXLinkURL = dialURL

	if err := s.dxClient.Authenticate(td.Token); err != nil {
		s.dxClient = nil
		s.dxStatus = DXLinkStatus{}
		s.dxMu.Unlock()
		return fmt.Errorf("ingress: authenticate: %w", err)
	}
	s.dxStatus.Authenticated = true

	if len(symbols) > 0 {
		if err := s.dxClient.Subscribe(symbols); err != nil {
			s.dxMu.Unlock()
			return fmt.Errorf("ingress: subscribe to %v: %w", symbols, err)
		}
		// Assume Quote for now — matches the single event field set
		// requested in dxclient.Subscribe. Phase 3 will broaden.
		for _, sym := range symbols {
			s.subs.Register("Quote", sym, subjectFor(sym))
		}
	}

	feed, _ := s.dxClient.C()
	s.dxMu.Unlock()

	go s.runIngressLoop(ctx, feed)
	return nil
}

// runIngressLoop consumes dxLink messages and publishes FEED_DATA events to
// NATS verbatim. Runs until ctx is canceled.
func (s *service) runIngressLoop(ctx context.Context, feed <-chan dx.Message) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-feed:
			if !ok {
				return
			}
			events, err := FeedEvents(msg)
			if err != nil {
				s.Log(int(slog.LevelError), "ingress: parse feed message", "err", err)
				continue
			}
			for _, ev := range events {
				if err := s.nats.Publish(ev.Subject, ev.Payload); err != nil {
					s.Log(int(slog.LevelError), "ingress: publish", "subject", ev.Subject, "err", err)
					continue
				}
				s.subs.Observe(ev.Event, ev.Symbol)
			}
		}
	}
}
