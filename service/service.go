package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

	"github.com/brojonat/godxfeed/dxclient"
	dx "github.com/brojonat/godxfeed/dxclient"
	"github.com/brojonat/godxfeed/service/api"
	"github.com/brojonat/godxfeed/service/db/dbgen"
	bwebsocket "github.com/brojonat/websocket"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.temporal.io/sdk/client"
	"gonum.org/v1/gonum/stat/distuv"
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
	// Log logs a message.
	Log(level int, m string, args ...any)
	// TemporalClient returns a Temporal client. N.B.: under certain
	// circumstances, callers may choose to initialize the service without a
	// Temporal client, in which case this will return nil.
	TemporalClient() client.Client
	// DBPool returns a *pgxpool.Pool. This is useful if you need to perform a
	// query that needs a transaction. N.B.: under certain circumstances,
	// callers may choose to initialize the service without a DB connection, in
	// which case this will return nil.
	DBPool() *pgxpool.Pool
	// DBQ returns a *dbgen.Queries. This is the main interface to the DB. N.B.:
	// under certain circumstances, callers may choose to initialize the service
	// without a DB connection, in which case this will return nil.
	DBQ() *dbgen.Queries

	// WSClientRegister registers a websocket connection (e.g., a browser).
	WSClientRegister(ctx context.Context, cf context.CancelFunc, c bwebsocket.Client)
	// WSClientUnregister unregisters a websocket connection.
	WSClientUnregister(c bwebsocket.Client)
	// WSClientHandleMessage returns a message handler. This is a function that
	// accept a bwebsocket.Client and a []byte that represents a message from
	// the client. This handler will be invoked on every message received from
	// the client. IMPORTANT: this implementation DEFINES the API for websocket
	// clients to interact with our service! The service is free to implement
	// any functionality it wants. The minimal recommended implementation is a
	// function that listens for a "subscribe" message from the client and
	// registers the client on the service as a listener. Additionally, the
	// implementation should also listen for an "unsubscribe" message from the
	// client and then deregister the client from the service. Do whatever you
	// want, but you should keep this as simple as possible!
	WSClientHandleMessage() bwebsocket.MessageHandler

	// NewSessionToken returns a new session token from the TastyTrade API.
	NewSessionToken(username string, password string) (*api.Response, error)
	// Tests the provided session token against the TastyTrade API.
	TestSessionToken(sessionToken string) (*api.Response, error)
	// Returns a token that can be used with the dxfeed streaming API.
	NewStreamerToken() (*api.Response, error)
	// Returns all the metadata for the symbol from the TastyTrade API.
	GetSymbolData(symbol string, symbolType string) (*api.Response, error)
	// Returns the option chain for the symbol from the TastyTrade API.
	GetOptionChain(symbol string) (*api.Response, error)
	// Returns the metadata for the provided equity symbol AND all related
	// option symbols
	GetRelatedSymbols(symbol string) ([]string, error)

	// Returns a channel that emits updates for the supplied symbols. This
	// should only be invoked ONCE per service instance since it generally
	// requires a new websocket connection to the dxfeed streaming API.
	StreamSymbols(ctx context.Context, syms []string) (<-chan []byte, error)
	// This handles all messages received from the dxfeed streaming API.
	// Implementations likely do things like discard/ignore control messages,
	// filter for FEED_DATA messages, and/or transform messages into richer data
	// structures before serializing and returning the result. Note that the
	// return type is []byte to accommodate any conceivable implementation.
	HandleDXFeedMessage(message dx.Message) ([]byte, error)

	// Subscribes the client to receive updates for the supplied symbol
	SubscribeClient(c bwebsocket.Client, symbol string) error
	// Unsubscribes the client from updates for the supplied symbol
	UnsubscribeClient(c bwebsocket.Client, symbol string) error
}

type service struct {
	twEndpoint    string
	sessionToken  string
	dxEndpoint    string
	streamerToken string
	logger        *slog.Logger
	tc            client.Client
	dbpool        *pgxpool.Pool
	dbqueries     *dbgen.Queries
	subLock       *sync.RWMutex
	subs          map[bwebsocket.Client]map[string]context.CancelFunc
}

func NewService(
	twEndpoint string,
	sessionToken string,
	dxEndpoint string,
	streamerToken string,
	l *slog.Logger,
	tc client.Client,
	p *pgxpool.Pool,
	q *dbgen.Queries,
) Service {
	s := &service{
		twEndpoint:    twEndpoint,
		sessionToken:  sessionToken,
		dxEndpoint:    dxEndpoint,
		streamerToken: streamerToken,
		logger:        l,
		tc:            tc,
		dbpool:        p,
		dbqueries:     q,
		subLock:       &sync.RWMutex{},
		subs:          make(map[bwebsocket.Client]map[string]context.CancelFunc),
	}
	return s
}

func (s *service) recordSymbolData(fds []dxclient.FeedCompactQuote) (int64, error) {
	ts := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	data := []dbgen.InsertSymbolDataParams{}
	for _, fd := range fds {
		data = append(data, dbgen.InsertSymbolDataParams{
			Symbol:   fd.EventSymbol,
			Ts:       ts,
			BidPrice: fd.BidPrice,
			BidSize:  fd.BidSize,
			AskPrice: fd.AskPrice,
			AskSize:  fd.AskSize,
		})
	}
	if len(data) == 0 {
		return 0, nil
	}
	return s.DBQ().InsertSymbolData(context.Background(), data)
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
func (s *service) WSClientUnregister(c bwebsocket.Client) {
	for _, cf := range s.subs[c] {
		cf()
	}
}
func (s *service) WSClientHandleMessage() bwebsocket.MessageHandler {
	return bwebsocket.MessageHandler(
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
					Topic string `json:"topic"`
				}
				if err := json.Unmarshal(cm.Body, &body); err != nil {
					s.Log(int(slog.LevelInfo), "unable to parse client body", "data", string(cm.Body))
					return
				}
				topic := strings.ToUpper(body.Topic)
				if err := s.SubscribeClient(c, topic); err != nil {
					s.Log(int(slog.LevelError), "unable to subscribe client", "error", err.Error(), "data", string(cm.Body))
					return
				}

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
	)
}

func (s *service) NewSessionToken(u, p string) (*api.Response, error) {
	url := fmt.Sprintf("https://%s/sessions", s.twEndpoint)
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

func (s *service) TestSessionToken(sessionToken string) (*api.Response, error) {
	url := fmt.Sprintf("https://%s/customers/me", s.twEndpoint)
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
	if response.Error.Code != "" {
		return nil, fmt.Errorf("tastytrade error for session token query: %s", response.Error.Message)
	}
	return &response, nil
}

func (s *service) NewStreamerToken() (*api.Response, error) {
	url := fmt.Sprintf("https://%s/api-quote-tokens", s.twEndpoint)
	r, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: could not create request: %w", api.ErrorInternal{Message: "internal error"}, err)
	}

	addDefaultHeaders(r.Header)
	r.Header.Add("Authorization", s.sessionToken)

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
	if response.Error.Code != "" {
		return nil, fmt.Errorf("tastytrade error for streamer token query: %s", response.Error.Message)
	}
	return &response, nil
}

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
	r.Header.Add("Authorization", s.sessionToken)

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
	r.Header.Add("Authorization", s.sessionToken)

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

// GetRelatedSymbols returns the equity and options symbols for the supplied equity symbol
// sorted by days to expiry.
func (s *service) GetRelatedSymbols(symbol string) ([]string, error) {
	// get the equity streamer symbols
	twr, err := s.GetSymbolData(symbol, api.SYMBOL_TYPE_EQUITIES)
	if err != nil {
		return []string{}, fmt.Errorf("could not get equity symbol data: %w", err)
	}
	var eqData api.EquitySymbol
	if err := json.Unmarshal(twr.Data, &eqData); err != nil {
		return []string{}, fmt.Errorf("could not unmarshal equity symbol data: %w", err)
	}
	syms := []string{eqData.StreamerSymbol}

	// get the option streamer symbols
	twr, err = s.GetSymbolData(symbol, api.SYMBOL_TYPE_OPTIONS)
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

func (s *service) SubscribeClient(c bwebsocket.Client, topic string) error {
	// Run a goroutine for each symbol the client subscribes to. When the
	// client unsubscribes from this symbol, the goroutine will be cancelled.
	ctx := context.Background()
	go func() {
		ctx, cf := context.WithCancel(ctx)
		defer cf()
		ch, err := s.getTopicChannel(ctx, topic)
		if err != nil {
			s.Log(int(slog.LevelError), fmt.Sprintf("unable to get topic channel: %v", err))
			return
		}
		if err := s.storeClientCF(c, cf, topic); err != nil {
			s.Log(int(slog.LevelError), fmt.Sprintf("unable to subscribe client: %v", err))
			return
		}

		// send all the data available up to present
		// for _, r := range rows {
		// 		b, err := json.Marshal(r)
		//  	if err != nil {
		//  		s.Log(int(slog.LevelError), "unable to serialize row")
		//  		continue
		//  	}
		//  	c.Write(b)
		// }

		// now send periodic updates as they come in
		for {
			select {
			case <-ctx.Done():
				s.Log(int(slog.LevelInfo), "client subscription context done; shutting down write loop")
				return
			case b := <-ch:
				c.Write(b)
			}
		}
	}()
	return nil
}

func (s *service) storeClientCF(c bwebsocket.Client, cf context.CancelFunc, symbol string) error {
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

func (s *service) getTopicChannel(ctx context.Context, topic string) (chan []byte, error) {
	lrng := distuv.LogNormal{
		Mu:    1,
		Sigma: 1,
		Src:   nil,
	}

	switch topic {
	case "TOPIC":
		ch := make(chan []byte, 1)
		go func() {
			t := time.NewTicker(100 * time.Millisecond)
			for {
				select {
				case <-ctx.Done():
					s.Log(int(slog.LevelInfo), "shutting down client subscription to TOPIC")
					return
				case <-t.C:
					payload := struct {
						Symbol string    `json:"symbol"`
						Value  float64   `json:"value"`
						TS     time.Time `json:"ts"`
					}{
						Symbol: "SPY",
						Value:  lrng.Rand(),
						TS:     time.Now(),
					}
					b, err := json.Marshal(payload)
					if err != nil {
						s.Log(int(slog.LevelError), "error serializing payload for random topic: %s", err)
					}
					ch <- b
				}
			}
		}()
		return ch, nil
	default:
		return nil, fmt.Errorf("unrecognized topic: %s", topic)
	}
}

// StreamSymbols connects to the DXLink websocket endpoint, authenticates, and
// subscribes to the supplied symbols for quote data. It returns a channel and
// an error. Any messages that the DXLink server sends are sent down the
// returned channel. Consumers can range over the channel; it will be closed
// when done reading data. NOTE: every call to StreamSymbols creates a new
// client that connects to the DXLink endpoint; this should only be called
// ONCE PER SERVICE INSTANCE. The service can write the data to a database
// and then we can serve the aggregate data to any number of clients.
func (s *service) StreamSymbols(
	ctx context.Context,
	syms []string,
) (<-chan []byte, error) {
	out := make(chan []byte)
	// Pass the service logger to the client for logging. This may be a
	// boneheaded move but I'm testing it out.
	logfunc := func(lvl int, msg string, args ...any) {
		s.Log(lvl, msg, args)
	}
	c := dx.NewClient(logfunc)
	noop := func(ms dx.MessageSetup) error { return nil }
	if err := c.Dial(ctx, "wss://"+s.dxEndpoint, noop); err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	if err := c.Authenticate(s.streamerToken); err != nil {
		return nil, fmt.Errorf("%w: %w", api.ErrorInternal{Message: "internal error"}, err)
	}
	c.Subscribe(syms)

	// In a separate goroutine, range over the client channel and send all the
	// relevant incoming message to the caller on the returned channel.
	data, _ := c.C()
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(out)
				return
			case msg := <-data:
				b, err := s.HandleDXFeedMessage(msg)
				switch err {
				case nil:
					// success; send the message and continue on
					out <- b
				case errNotFeedData:
					// message was not a FEED_DATA message; may have been some
					// sort of control message or something, just no-op
				default:
					// we encountered some unexpected error; log it and continue
					s.Log(
						int(slog.LevelInfo),
						"unexpected error handling message from dxfeed",
						"err", err.Error(),
						"msg", msg,
					)
				}
			}
		}
	}()
	return out, nil
}

var errNotFeedData = errors.New("not a FEED_DATA message")

// Accepts a message from the dxfeed. It modifies the supplied message,
// potentially adding useful information such as theoretical prices, and returns
// the resulting bytes. Similarly, this function may return an error if the
// message should not be forwarded to subscribed clients (such as when the
// message is merely a KEEPALIVE control message).
func (s *service) HandleDXFeedMessage(m dx.Message) ([]byte, error) {
	msg, ok := m.(dx.MessageFeedData)
	if !ok {
		return nil, errNotFeedData
	}

	// NOTE: some numeric fields coming from dxfeed are set to "NaN", which will
	// result in an error if we try to parse them. Fortunately, we can simply
	// replace those bytes with a suitable zero value (i.e., 0).
	msg.Data = bytes.ReplaceAll(msg.Data, []byte(`"NaN"`), []byte(`0.0`))

	var data []dx.FeedCompactQuote
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		return nil, fmt.Errorf("could not deserialize FEED_DATA data (%w): %s", err, msg.Data)
	}

	count, err := s.recordSymbolData(data)
	if err != nil {
		s.Log(
			int(slog.LevelError),
			"error inserting feed data into db",
			"error", err.Error(),
			"count", count,
		)
	}

	res := []api.FeedCompactQuote{}
	for _, d := range data {
		res = append(res, api.FeedCompactQuote{
			FeedCompactQuote: dx.FeedCompactQuote{
				AskPrice:    d.AskPrice,
				AskSize:     d.AskSize,
				BidPrice:    d.BidPrice,
				BidSize:     d.BidSize,
				EventSymbol: d.EventSymbol,
				EventType:   d.EventType,
			},
			AskPriceTheo: 123,
			BidPriceTheo: 789,
		})
	}
	b, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("could not serialize updated data")
	}

	return b, nil
}
