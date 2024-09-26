package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"

	dx "github.com/brojonat/godxfeed/dxclient"
	"github.com/brojonat/godxfeed/service/api"
	"github.com/brojonat/godxfeed/service/db/dbgen"
	bwebsocket "github.com/brojonat/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
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

type Service interface {
	Log(level int, m string, args ...any)
	TemporalClient() client.Client
	DBPool() *pgxpool.Pool
	DBQ() *dbgen.Queries

	WSClientRegister(ctx context.Context, cf context.CancelFunc, c bwebsocket.Client)
	WSClientUnregister(c bwebsocket.Client)
	WSClientHandleMessage(twEndpoint string, twToken string, dxEndpoint string, dxToken string) []bwebsocket.MessageHandler

	NewSessionToken(endpoint string, username string, password string) (*api.Response, error)
	TestSessionToken(endpoint string, sessionToken string) (*api.Response, error)
	NewStreamerToken(endpoint string, sessionToken string) (*api.Response, error)
	GetSymbolData(endpoint string, sessionToken string, symbol string, symbolType string) (*api.Response, error)
	GetOptionChain(endpoint string, sessionToken string, symbol string, nested bool, compact bool) (*api.Response, error)

	GetRelatedSymbols(endpoint string, sessionToken string, symbol string) ([]string, error)
	SubscribeClient(c bwebsocket.Client, cf context.CancelFunc, symbol string) error
	UnsubscribeClient(c bwebsocket.Client, symbol string) error
	StreamSymbols(ctx context.Context, endpoint string, token string, syms []string) (<-chan []byte, error)
	PrepareMessageForClient(symbols []string, message dx.Message, validators ...func([]string, dx.Message) error) ([]byte, error)
}

type service struct {
	logger    *slog.Logger
	tc        client.Client
	dbpool    *pgxpool.Pool
	dbqueries *dbgen.Queries
	subLock   *sync.RWMutex
	subs      map[bwebsocket.Client]map[string]context.CancelFunc
}

func NewService(l *slog.Logger, tc client.Client, p *pgxpool.Pool, q *dbgen.Queries) Service {
	return &service{
		logger:    l,
		tc:        tc,
		dbpool:    p,
		dbqueries: q,
		subLock:   &sync.RWMutex{},
		subs:      make(map[bwebsocket.Client]map[string]context.CancelFunc),
	}
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

func (s *service) TemporalClient() client.Client {
	return s.tc
}

func (s *service) DBPool() *pgxpool.Pool {
	return s.dbpool
}

func (s *service) DBQ() *dbgen.Queries {
	return s.dbqueries
}

func (s *service) WSClientRegister(ctx context.Context, cf context.CancelFunc, c bwebsocket.Client) {}
func (s *service) WSClientUnregister(c bwebsocket.Client)                                           {}
func (s *service) WSClientHandleMessage(twEndpoint, twToken, dxEndpoint, dxToken string) []bwebsocket.MessageHandler {
	return []bwebsocket.MessageHandler{
		func(c bwebsocket.Client, b []byte) {
			s.Log(int(slog.LevelInfo), fmt.Sprintf("got client message: %s", b))

			var cm api.ClientMessage
			if err := json.Unmarshal(b, &cm); err != nil {
				s.Log(int(slog.LevelInfo), "unable to parse client message", "data", string(b))
				return
			}

			switch cm.Type {
			case api.CLIENT_MESSAGE_TYPE_SUBSCRIBE:
				var body struct {
					Symbol string `json:"symbol"`
				}
				if err := json.Unmarshal(cm.Body, &body); err != nil {
					s.Log(int(slog.LevelInfo), "unable to parse client body", "data", string(cm.Body))
					return
				}
				sym := strings.ToUpper(body.Symbol)

				// Run a goroutine for each symbol the client subscribes to. When the
				// client unsubscribes from this symbol, the goroutine will be cancelled.
				ctx := context.Background()
				go func() {
					ctx, cf := context.WithCancel(ctx)
					ch, err := s.StreamSymbols(ctx, dxEndpoint, dxToken, []string{sym})
					if err != nil {
						s.Log(int(slog.LevelError), fmt.Sprintf("unable to stream symbols: %v", err))
						cf()
						return
					}
					if err := s.SubscribeClient(c, cf, sym); err != nil {
						s.Log(int(slog.LevelError), fmt.Sprintf("unable to subscribe client: %v", err))
						return
					}
					for {
						select {
						case <-ctx.Done():
							return
						case b := <-ch:
							c.Write(b)
						}
					}
				}()

			case api.CLIENT_MESSAGE_TYPE_UNSUBSCRIBE:
				s.Log(int(slog.LevelInfo), "got unsubscribe message", "data", string(cm.Body))
				var body struct {
					Symbol string `json:"symbol"`
				}
				if err := json.Unmarshal(cm.Body, &body); err != nil {
					s.Log(int(slog.LevelInfo), "unable to parse client body", "data", string(cm.Body))
					return
				}
				sym := strings.ToUpper(body.Symbol)
				if err := s.UnsubscribeClient(c, sym); err != nil {
					s.Log(int(slog.LevelError), fmt.Sprintf("unable to unsubscribe client: %v", err))
					return
				}
			default:
				s.Log(int(slog.LevelDebug), "unhandled message type", "data", string(b))
			}
		},
	}
}

func (s *service) NewSessionToken(endpoint, u, p string) (*api.Response, error) {
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
		return nil, fmt.Errorf("%w: could not marshal request body: %w", api.ErrorBadRequest{Message: "bad request"}, err)
	}

	r, err := http.NewRequest(http.MethodPost, url, bytes.NewBuffer(b))
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)

	}

	addDefaultHeaders(r.Header)

	res, err := http.DefaultClient.Do(r)
	if err != nil {
		return nil, fmt.Errorf("%w: could not do request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	defer res.Body.Close()

	b, err = io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: could not read response body: %w", api.ErrorInternal{Message: "internal error"}, err)
	}

	var response api.Response
	if err := json.Unmarshal(b, &response); err != nil {
		return nil, fmt.Errorf("%w: could not parse response body: %w", api.ErrorInternal{Message: "internal error"}, err)
	}

	switch res.StatusCode {
	case http.StatusOK, http.StatusCreated:
	default:
		return nil, fmt.Errorf("%w: unexpected response: %s\n%s", api.ErrorInternal{Message: "internal error"}, res.Status, b)
	}
	return &response, nil
}

func (s *service) TestSessionToken(endpoint, sessionToken string) (*api.Response, error) {
	url := fmt.Sprintf("https://%s/customers/me", endpoint)
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}

	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

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
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s\n%s", api.ErrorBadRequest{Message: "bad auth credentials"}, res.Status, b)
	default:
		return nil, fmt.Errorf("%w: unexpected response: %s\n%s", api.ErrorInternal{Message: "internal error"}, res.Status, b)
	}
	return &response, nil
}

func (s *service) NewStreamerToken(endpoint, sessionToken string) (*api.Response, error) {
	url := fmt.Sprintf("https://%s/api-quote-tokens", endpoint)
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}

	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

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
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, fmt.Errorf("%w: %s\n%s", api.ErrorBadRequest{Message: "bad auth credentials"}, res.Status, b)
	default:
		return nil, fmt.Errorf("%w: unexpected response: %s\n%s", api.ErrorInternal{Message: "internal error"}, res.Status, b)
	}
	return &response, nil
}

func (s *service) GetSymbolData(endpoint, sessionToken, symbol, symbolType string) (*api.Response, error) {
	sym := strings.ToUpper(symbol)
	symType := strings.ToLower(symbolType)
	url, err := getSymbologyURL(endpoint, symType, sym)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorBadRequest{Message: "bad request"}, err)
	}
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

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
	return &response, nil
}

func (s *service) GetOptionChain(endpoint, sessionToken, symbol string, nested, compact bool) (*api.Response, error) {
	sym := strings.ToUpper(symbol)
	url := fmt.Sprintf("https://%s/option-chains/%s", endpoint, sym)
	if nested {
		url += "/nested"
	} else if compact {
		url += "/compact"
	}
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", sessionToken)

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
	return &response, nil
}

// GetRelatedSymbols returns the equity and options symbols for the supplied equity symbol
// sorted by days to expiry.
func (s *service) GetRelatedSymbols(endpoint, token, symbol string) ([]string, error) {
	// get the equity streamer symbols
	twr, err := s.GetSymbolData(
		endpoint,
		token,
		symbol,
		api.SYMBOL_TYPE_EQUITIES,
	)
	if err != nil {
		return []string{}, fmt.Errorf("could not get equity symbol data: %w", err)
	}
	var eqData api.EquitySymbol
	if err := json.Unmarshal(twr.Data, &eqData); err != nil {
		return []string{}, fmt.Errorf("could not unmarshal equity symbol data: %w", err)
	}
	syms := []string{eqData.StreamerSymbol}

	// get the option streamer symbols
	twr, err = s.GetSymbolData(
		endpoint,
		token,
		symbol,
		api.SYMBOL_TYPE_OPTIONS,
	)
	if err != nil {
		return []string{}, fmt.Errorf("could not get option symbology data: %w", err)
	}
	var optsData struct {
		Items []api.OptionSymbol `json:"items"`
	}
	if err := json.Unmarshal(twr.Data, &optsData); err != nil {
		return []string{}, fmt.Errorf("could not unmarshal option symbol data: %w", err)
	}
	// sort options symbols by (expiry, strike)
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
	for _, item := range optsData.Items {
		syms = append(syms, item.StreamerSymbol)
	}
	return syms, nil
}

func (s *service) SubscribeClient(c bwebsocket.Client, cf context.CancelFunc, symbol string) error {
	s.subLock.Lock()
	defer s.subLock.Unlock()
	_, ok := s.subs[c]
	if !ok {
		s.subs[c] = map[string]context.CancelFunc{}
	}
	s.subs[c][symbol] = cf
	return nil
}

func (s *service) UnsubscribeClient(c bwebsocket.Client, symbol string) error {
	s.subLock.Lock()
	defer s.subLock.Unlock()
	_, ok := s.subs[c]
	if !ok {
		return fmt.Errorf("client has no active subscriptions")
	}
	cf, ok := s.subs[c][symbol]
	if !ok {
		return fmt.Errorf("client not subscribed to %s", symbol)
	}
	cf()
	delete(s.subs[c], symbol)
	return nil
}

// StreamSymbols connects to the DXLink websocket endpoint, authenticates, and
// subscribes to the supplied symbols for quote data. It returns a channel and
// an error. Any messages that the DXLink server sends are sent down the
// returned channel. Consumers can range over the channel; it will be closed
// when done reading data. NOTE: every call to StreamSymbols creates a new
// client that connects to the DXLink endpoint; this should only be called once
// per subscriber.
func (s *service) StreamSymbols(ctx context.Context, endpoint, token string, syms []string) (<-chan []byte, error) {
	url := fmt.Sprintf("wss://%s", endpoint)
	out := make(chan []byte)
	c := dx.NewClient()
	noop := func(ms dx.MessageSetup) error { return nil }
	if err := c.Dial(ctx, url, noop); err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	if err := c.Authenticate(token); err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorInternal{Message: "internal error"}, err)
	}

	c.Subscribe(syms)

	data, _ := c.C()
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(out)
				return
			case msg := <-data:
				b, err := s.PrepareMessageForClient(syms, msg)
				if err != nil {
					s.Log(int(slog.LevelInfo), err.Error())
					continue
				}
				out <- b
			}
		}
	}()
	return out, nil
}

// PrepareMessageForClient accepts a list of symbols that the client has subscribed to
// and a message from the corresponding subscription. It modifies the supplied message
// and returns the result. The canonical use case is adding theoretical prices to
// option price quotes, though other implementations may perform different modifications.
// Similarly, this function may return an error if the message should not be forwarded
// to the subscription (such as when a KEEPALIVE message is received).
func (s *service) PrepareMessageForClient(syms []string, m dx.Message, validators ...func([]string, dx.Message) error) ([]byte, error) {
	// FIXME: turn these into validators we can inject
	b, err := m.JSON()
	if err != nil {
		s.Log(int(slog.LevelError), fmt.Sprintf("could not serialize message, %v", err))
		return nil, err
	}
	var mb dx.MessageBase
	if err := json.Unmarshal(b, &mb); err != nil {
		s.Log(int(slog.LevelError), fmt.Sprintf("could not parse message: %v", err))
		return nil, err
	}

	// we only want to send FEED_DATA messages that pertain to symbols in the subscription list
	if mb.Type != dx.MESSAGE_TYPE_FEED_DATA {
		return nil, fmt.Errorf("not a FEED_DATA message")
	}
	msg, ok := m.(dx.MessageFeedData)
	if !ok {
		return nil, fmt.Errorf("could not assert FEED_DATA message")
	}
	// data is a list of structs
	type original struct {
		AskPrice    float64 `json:"askPrice"`
		AskSize     float64 `json:"askSize"`
		BidPrice    float64 `json:"bidPrice"`
		BidSize     float64 `json:"bidSize"`
		EventSymbol string  `json:"eventSymbol"`
		EventType   string  `json:"eventType"`
	}
	var data []original
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return nil, fmt.Errorf("could not deserialize FEED_DATA data: %s", msg.Data)
	}

	// FIXME: unscope this?
	type updated struct {
		AskPrice     float64 `json:"askPrice"`
		AskPriceTheo float64 `json:"askPriceTheo"`
		AskSize      float64 `json:"askSize"`
		BidPrice     float64 `json:"bidPrice"`
		BidPriceTheo float64 `json:"bidPriceTheo"`
		BidSize      float64 `json:"bidSize"`
		EventSymbol  string  `json:"eventSymbol"`
		EventType    string  `json:"eventType"`
	}
	nm := []updated{}
	for _, d := range data {
		// not subscribed to this symbol
		if !slices.Contains(syms, d.EventSymbol) {
			s.Log(int(slog.LevelError), "received data the client didn't subscribe to")
			return nil, fmt.Errorf("symbol not subscribed to")
		}
		// FIXME: in the future once we're receiving options data, we'll need to filter
		// here to options symbols
		nm = append(nm, updated{AskPrice: d.AskPrice, AskPriceTheo: 123, AskSize: d.AskSize, BidPrice: d.BidPrice, BidPriceTheo: 789, BidSize: d.BidSize, EventSymbol: d.EventSymbol, EventType: d.EventType})
	}
	b, err = json.Marshal(nm)
	if err != nil {
		s.Log(int(slog.LevelError), fmt.Sprintf("could not serialize updated data: %v", err))
		return nil, fmt.Errorf("could not serialize updated data")
	}
	return b, nil
}
