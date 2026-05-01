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
	"time"

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

	// StartIngress dials dxLink, authenticates, opens the feed channel,
	// subscribes to the initial symbols, and begins publishing FEED_DATA
	// events to NATS. Safe to call once per service instance.
	StartIngress(ctx context.Context, symbols []string) error

	// AddSubscription adds a single (event, symbol) subscription on the
	// live dxLink feed. Errors if StartIngress hasn't run yet.
	AddSubscription(event, symbol string) error
	// RemoveSubscription removes a single (event, symbol) subscription.
	RemoveSubscription(event, symbol string) error
	// BulkAddSubscriptions adds a batch of (event, symbol) pairs in a
	// single wire call. Used by `/nl-subscribe` to apply a resolved
	// FilterSpec in one shot — cheaper than N AddSubscription calls.
	// Pairs already present are silently skipped (same semantics as
	// Add). Errors if StartIngress hasn't run yet.
	BulkAddSubscriptions(pairs []struct{ Event, Symbol string }) error

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
	// dxForceURL, if non-empty, overrides the dial URL returned by
	// tastytrade's /api-quote-tokens and also short-circuits the OAuth
	// streamer-token fetch. Used to point the ingress at a local mock
	// (e.g. tools/synth) for offline validation.
	dxForceURL string
	tp         TokenProvider
	logger     *slog.Logger
	nats       *nats.Conn
	subs       *SubscriptionManager

	// dxLink connection state. Everything inside here is guarded by dxMu.
	dxMu           sync.RWMutex
	dxClient       dx.Client
	dxClientCancel context.CancelFunc
	dxStatus       DXLinkStatus
}

// NewService constructs the service. Note the absence of a *pgxpool / DB
// handle — persistence is delegated entirely to analytics sinks that have
// their own connections. The SubscriptionManager is deferred until
// StartIngress, when the dxLink client it depends on exists.
//
// dxForceURL, when non-empty, forces StartIngress to dial that exact URL
// (scheme included, e.g. ws://localhost:9999/realtime) and to skip the
// tastytrade streamer-token fetch. Pass "" for real-mode operation.
func NewService(
	twEndpoint string,
	dxEndpoint string,
	dxForceURL string,
	tp TokenProvider,
	l *slog.Logger,
	nc *nats.Conn,
) Service {
	return &service{
		twEndpoint: twEndpoint,
		dxEndpoint: dxEndpoint,
		dxForceURL: dxForceURL,
		tp:         tp,
		logger:     l,
		nats:       nc,
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

func (s *service) Subscriptions() []SubscriptionInfo {
	s.dxMu.RLock()
	m := s.subs
	s.dxMu.RUnlock()
	if m == nil {
		return nil
	}
	return m.Snapshot()
}

// AddSubscription adds an (event, symbol) to the live dxLink feed.
func (s *service) AddSubscription(event, symbol string) error {
	s.dxMu.RLock()
	m := s.subs
	s.dxMu.RUnlock()
	if m == nil {
		return fmt.Errorf("AddSubscription: ingress not started")
	}
	return m.Add(event, symbol)
}

// RemoveSubscription drops an (event, symbol) from the live dxLink feed.
func (s *service) RemoveSubscription(event, symbol string) error {
	s.dxMu.RLock()
	m := s.subs
	s.dxMu.RUnlock()
	if m == nil {
		return fmt.Errorf("RemoveSubscription: ingress not started")
	}
	return m.Remove(event, symbol)
}

// BulkAddSubscriptions adds a batch of (event, symbol) pairs in a
// single wire call. Existing pairs are silently skipped.
func (s *service) BulkAddSubscriptions(pairs []struct{ Event, Symbol string }) error {
	s.dxMu.RLock()
	m := s.subs
	s.dxMu.RUnlock()
	if m == nil {
		return fmt.Errorf("BulkAddSubscriptions: ingress not started")
	}
	return m.BulkAdd(pairs)
}

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
// every incoming FEED_DATA event to NATS verbatim. Returns after the first
// connection is established; the loop runs until ctx is canceled, reconnecting
// with exponential backoff on connection drop.
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

	feed, err := s.dialAndSubscribe(ctx)
	if err != nil {
		s.dxMu.Unlock()
		return fmt.Errorf("ingress: %w", err)
	}
	s.dxMu.Unlock()

	// Register the initial symbols in a single wire call.
	if len(symbols) > 0 {
		pairs := make([]struct{ Event, Symbol string }, 0, len(symbols))
		for _, sym := range symbols {
			pairs = append(pairs, struct{ Event, Symbol string }{Event: "Quote", Symbol: sym})
		}
		if err := s.subs.BulkAdd(pairs); err != nil {
			return fmt.Errorf("ingress: initial subscribe: %w", err)
		}
	}

	go s.ingressWithReconnect(ctx, feed)
	return nil
}

// dialAndSubscribe creates a fresh dxLink client, dials, authenticates,
// and opens the feed channel. On first call it creates a new
// SubscriptionManager; on reconnect it rebinds the existing one to the
// new client and replays all active subscriptions. Caller must hold
// s.dxMu for write.
func (s *service) dialAndSubscribe(ctx context.Context) (<-chan dx.Message, error) {
	// Cancel the previous client's goroutines if any.
	if s.dxClientCancel != nil {
		s.dxClientCancel()
	}

	clientCtx, clientCancel := context.WithCancel(ctx)

	cl := func(level int, msg string, args ...any) { s.Log(level, msg, args...) }
	client := dx.NewClient(cl)

	// Mock-mode: dxForceURL bypasses the tastytrade streamer-token round
	// trip entirely. Any token works — tools/synth noop-authorizes.
	var dialURL, authToken string
	if s.dxForceURL != "" {
		dialURL = s.dxForceURL
		authToken = "mock"
	} else {
		td, err := s.NewStreamerToken(ctx)
		if err != nil {
			clientCancel()
			return nil, fmt.Errorf("get streamer token: %w", err)
		}
		dialURL = td.DXLinkURL
		if dialURL == "" {
			dialURL = "wss://" + s.dxEndpoint
		}
		authToken = td.Token
	}
	s.Log(int(slog.LevelInfo), "dialing dxlink", "url", dialURL, "mock", s.dxForceURL != "")

	if err := client.Dial(clientCtx, dialURL, func(msg dx.MessageSetup) error {
		s.Log(int(slog.LevelDebug), "dxlink setup reply", "msg", fmt.Sprintf("%+v", msg))
		return nil
	}); err != nil {
		clientCancel()
		return nil, fmt.Errorf("dial: %w", err)
	}

	if err := client.Authenticate(authToken); err != nil {
		clientCancel()
		return nil, fmt.Errorf("authenticate: %w", err)
	}

	if err := client.OpenFeed(); err != nil {
		clientCancel()
		return nil, fmt.Errorf("open feed: %w", err)
	}

	// Rebind existing SubscriptionManager or create a new one.
	if s.subs != nil {
		if err := s.subs.Rebind(client); err != nil {
			clientCancel()
			return nil, fmt.Errorf("rebind subscriptions: %w", err)
		}
	} else {
		s.subs = NewSubscriptionManager(client)
	}

	feed, _ := client.C()
	s.dxClient = client
	s.dxClientCancel = clientCancel
	s.dxStatus = DXLinkStatus{
		Connected:     true,
		Authenticated: true,
		DXLinkURL:     dialURL,
	}
	return feed, nil
}

// ingressWithReconnect runs the ingress loop and reconnects with
// exponential backoff when the connection drops. Exits only when
// ctx is cancelled.
func (s *service) ingressWithReconnect(ctx context.Context, feed <-chan dx.Message) {
	for {
		s.runIngressLoop(ctx, feed)

		if ctx.Err() != nil {
			s.Log(int(slog.LevelInfo), "ingress: context cancelled, not reconnecting")
			s.dxMu.Lock()
			s.dxStatus = DXLinkStatus{}
			s.dxMu.Unlock()
			return
		}

		// Connection dropped. Tear down old client.
		s.Log(int(slog.LevelWarn), "ingress: connection lost, starting reconnect")
		s.dxMu.Lock()
		if s.dxClientCancel != nil {
			s.dxClientCancel()
		}
		oldClient := s.dxClient
		s.dxClient = nil
		s.dxStatus = DXLinkStatus{}
		s.dxMu.Unlock()

		if oldClient != nil {
			oldClient.Wait()
		}

		// Reconnect with backoff.
		var err error
		feed, err = s.reconnectWithBackoff(ctx)
		if err != nil {
			s.Log(int(slog.LevelInfo), "ingress: reconnect aborted", "err", err)
			return
		}
	}
}

// reconnectWithBackoff retries dialAndSubscribe until it succeeds or ctx
// is cancelled. Returns the new feed channel on success.
func (s *service) reconnectWithBackoff(ctx context.Context) (<-chan dx.Message, error) {
	b := newBackoff()
	for {
		delay := b.next()
		s.Log(int(slog.LevelInfo), "ingress: reconnecting",
			"delay", delay, "attempt", b.attempt)

		s.dxMu.Lock()
		s.dxStatus.ReconnectAttempt = b.attempt
		s.dxMu.Unlock()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}

		s.dxMu.Lock()
		feed, err := s.dialAndSubscribe(ctx)
		if err != nil {
			s.dxStatus.LastError = err.Error()
			s.dxMu.Unlock()
			s.Log(int(slog.LevelError), "ingress: reconnect attempt failed",
				"attempt", b.attempt, "err", err)
			continue
		}
		s.dxMu.Unlock()

		s.Log(int(slog.LevelInfo), "ingress: reconnected successfully",
			"attempt", b.attempt)
		return feed, nil
	}
}

// runIngressLoop consumes dxLink messages and publishes FEED_DATA events to
// NATS verbatim. Exits when the feed channel closes (connection dropped) or
// ctx is cancelled.
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
