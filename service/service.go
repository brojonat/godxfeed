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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	dx "github.com/brojonat/godxfeed/dxclient"
	"github.com/brojonat/godxfeed/service/api"
	"github.com/brojonat/godxfeed/service/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
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
	// NATS returns a *nats.Conn. This is the main interface to the NATS
	// server. N.B.: under certain circumstances, callers may choose to initialize
	// the service without a NATS connection, in which case this will return nil.
	NATS() *nats.Conn

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
	// Returns all related symbols for the provided symbol.
	GetRelatedOptionSymbols(symbol string) ([]string, error)
	// Returns the metadata for the provided equity symbol AND all related
	// option symbols that are fectched by calling the provided method. This package
	// Exports two helper factory methods:
	//
	// - "RelatedN": callback fetches the first n related symbols from the TastyTrade API.
	// NOTE: you can call this with 0 if you want to stream a single symbol and not
	// any related symbols.
	GetStreamSymbols(symbol string, method func(string) ([]string, error)) ([]string, error)

	// Returns a channel that emits APIFeedCompactQuoteData updates for the
	// supplied symbols. This should only be invoked ONCE per service instance
	// since it generally requires a new websocket connection to the dxfeed
	// streaming API.
	StreamAPIFeedCompactQuoteData(ctx context.Context, syms []string) (<-chan []api.FeedCompactQuote, error)

	// StreamHistoricalSymbolData writes the historical data for the provided symbol to
	// NATS.
	StreamHistoricalSymbolData(ctx context.Context, symbol string) error

	// RecordSymbolData writes the symbol data to the database.
	RecordSymbolData(data []byte) (int64, error)
	// PublishSymbolData publishes the symbol data to NATS.
	PublishSymbolData(data []byte) error

	// StopSymbolStream stops streaming for the provided symbol.
	StopSymbolStream(symbol string)
	// StopAllStreams stops all streams.
	StopAllStreams()

	// IsSymbolStreamActive returns true if the symbol is currently being streamed
	IsSymbolStreamActive(symbol string) bool
}

// SymbolMethodNRelatedOptions returns a callback that fetches the first n
// related option symbols from the TastyTrade API. If called with 0, this
// returns immediately. This is useful if you want to stream a single symbol and
// n of its related symbols. Note that you should limit the number of symbols
// you request to avoid overwhelming the TastyTrade API (or rather, the number
// of symbols you subscribe to at once). I've tested this with up to 50 symbols
// and it works fine, Maybe you can go higher but I haven't tested it. If you
// need more than 50 symbols, you may want to consider spinning up multiple
// service instances of the service (or otherwise implement a service that
// supports multiple connections). When that time comes, you'll likely want to
// implement your own symbol fetching method(s) so that you can more precisely
// page through the results. You can follow this pattern as an example.
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
	twEndpoint    string
	sessionToken  string
	dxEndpoint    string
	streamerToken string
	logger        *slog.Logger
	tc            client.Client
	dbpool        *pgxpool.Pool
	dbqueries     *dbgen.Queries
	nats          *nats.Conn

	// Add mutex to protect the streams map
	streamsMu sync.RWMutex
	// Map of symbol to cancel function for active streams
	streams map[string]context.CancelFunc

	// Add dx client fields
	dxClientMu sync.RWMutex
	dxClient   dx.Client
	dxChan     <-chan dx.Message
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
	nc *nats.Conn,
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
		nats:          nc,
		streams:       make(map[string]context.CancelFunc),
	}
	return s
}

// RecordSymbolData writes the symbol data to the database. Currently it
// expects a slice of FeedCompactQuote objects, but this may change in the
// future. It returns the number of rows inserted.
func (s *service) RecordSymbolData(data []byte) (int64, error) {
	if s.DBQ() == nil {
		return 0, nil
	}

	// parse from []byte to []dx.FeedCompactQuote
	var fds []dx.FeedCompactQuote
	if err := json.Unmarshal(data, &fds); err != nil {
		return 0, fmt.Errorf("could not unmarshal feed data: %w", err)
	}

	ts := pgtype.Timestamptz{Time: time.Now(), Valid: true}
	inserts := []dbgen.InsertSymbolDataParams{}
	for _, fd := range fds {
		inserts = append(inserts, dbgen.InsertSymbolDataParams{
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
	return s.DBQ().InsertSymbolData(context.Background(), inserts)
}

// PublishSymbolData publishes the symbol data to NATS. Currently it expects
// a slice of FeedCompactQuote objects, but this may change in the future.
func (s *service) PublishSymbolData(data []byte) error {
	if s.NATS() == nil {
		return nil
	}
	// parse from []byte to []dx.FeedCompactQuote
	var fds []dx.FeedCompactQuote
	if err := json.Unmarshal(data, &fds); err != nil {
		return fmt.Errorf("could not unmarshal feed data: %w", err)
	}

	for i, fd := range fds {
		b, err := json.Marshal(fd)
		if err != nil {
			return fmt.Errorf("could not marshal feed data: %w (skipping %d of %d)", err, len(fds)-i, len(fds))
		}
		err = s.NATS().Publish(fmt.Sprintf("godxfeed.%s", fd.EventSymbol), b)
		if err != nil {
			return fmt.Errorf("error publishing feed data: %w (skipping %d of %d)", err, len(fds)-i, len(fds))
		}
	}
	return nil
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

func (s *service) NATS() *nats.Conn {
	return s.nats
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

// GetSymbolData returns the symbol data for the supplied symbol and symbol type.
// Valid symbol types are "stock", "option", "future", and "index".
// Symbol should be an equity symbol.
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

// GetRelatedOptionSymbols returns the options symbols for the supplied equity symbol
// sorted by days to expiry.
func (s *service) GetRelatedOptionSymbols(symbol string) ([]string, error) {

	// get the option streamer symbols
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
	syms := []string{}
	for _, item := range optsData.Items {
		syms = append(syms, item.StreamerSymbol)
	}
	return syms, nil
}

func (s *service) GetStreamSymbols(symbol string, getRelateds func(string) ([]string, error)) ([]string, error) {
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

	relateds, err := getRelateds(symbol)
	if err != nil {
		return nil, fmt.Errorf("could not get related stream symbols: %w", err)
	}
	syms = append(syms, relateds...)
	return syms, nil
}

// StreamAPIFeedCompactQuoteData connects to the DXLink websocket endpoint,
// authenticates, and subscribes to the supplied symbols for quote data. It
// returns a channel and an error. Consumers can range over the channel; it will
// be closed when done reading data. All calls to this method use the same
// underlying dxlink client, but each call makes a subscription call to the
// dxlink server and creates a new channel to receive messages.
func (s *service) StreamAPIFeedCompactQuoteData(ctx context.Context, syms []string) (<-chan []api.FeedCompactQuote, error) {
	// Initialize dx client if not already done
	s.dxClientMu.Lock()
	if s.dxClient == nil {
		cl := func(level int, msg string, args ...any) {
			s.Log(level, msg, args...)
		}
		s.dxClient = dx.NewClient(cl)
		dc, handlerID := s.dxClient.C()
		s.dxChan = dc
		s.logger.Info("dx client channel", "handlerID", handlerID)

		fmt.Println("dialing dxlink", s.dxEndpoint)
		err := s.dxClient.Dial(ctx, fmt.Sprintf("wss://%s", s.dxEndpoint), func(msg dx.MessageSetup) error {
			fmt.Println("msg", msg)
			return nil
		})
		if err != nil {
			s.dxClientMu.Unlock()
			return nil, fmt.Errorf("could not dial dxlink: %w", err)
		}
		err = s.dxClient.Authenticate(s.streamerToken)
		if err != nil {
			s.dxClientMu.Unlock()
			return nil, fmt.Errorf("could not authenticate: %w", err)
		}
	}
	s.dxClientMu.Unlock()

	// Subscribe to symbols. I *THINK* that the dxlink server symbol subscriptions
	// are idempotent, so we don't need to handle that here. At some point we do
	// need to handle the case where the server rejects a subscription (e.g.,
	// too many subscriptions).
	err := s.dxClient.Subscribe(syms)
	if err != nil {
		return nil, fmt.Errorf("could not subscribe to symbols: %w", err)
	}

	// Get a new stream of feed messages (this is every message from the dxlink server
	// for all subscriptions handled by this service instance). Then, in a separate
	// goroutine, filter the FEED_DATA messages and send them down the streaming
	// channel returned to the caller.
	feed, _ := s.dxClient.C()
	out := make(chan []api.FeedCompactQuote)
	go func() {
		defer close(out)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-feed:
				if !ok {
					return
				}
				feeds, err := FilterAPIFeedCompactQuoteData(msg)
				if err != nil {
					s.Log(int(slog.LevelError), "error filtering feed data: %v", err)
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- feeds:
				}
			}
		}
	}()

	return out, nil
}

// FilterAPIFeedCompactQuoteData filters the FEED_DATA message from the DXLink
// server and returns a slice of api.FeedCompactQuote structs. Any other message
// types will result in an error.
func FilterAPIFeedCompactQuoteData(msg dx.Message) ([]api.FeedCompactQuote, error) {
	m, err := msg.JSON()
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal message: %w", err)
	}

	// NOTE: some numeric fields coming from dxfeed are set to "NaN", which will
	// result in an error if we try to parse them. Fortunately, we can simply
	// replace those bytes with a suitable zero value (i.e., 0).
	m = bytes.ReplaceAll(m, []byte(`"NaN"`), []byte(`0.0`))

	// {\"type\":\"FEED_DATA\",\"channel\":1,\"data\":[{\"eventType\":\"Quote\",\"eventSymbol\":\"SPY\",\"bidPrice\":576.39,\"askPrice\":576.42,\"bidSize\":185.0,\"askSize\":200.0}]}
	var feedMsg dx.MessageFeedData
	if err := json.Unmarshal(m, &feedMsg); err != nil {
		return nil, fmt.Errorf("could not deserialize message (%w): %s", err, m)
	}
	var data []dx.FeedCompactQuote
	if err := json.Unmarshal(feedMsg.Data, &data); err != nil {
		return nil, fmt.Errorf("could not deserialize feed data (%w): %s", err, feedMsg.Data)
	}

	// parse the data into our own api.FeedCompactQuote struct
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
	return res, nil
}

// StreamHistoricalSymbolData writes the historical data for the provided symbol to
// NATS.
func (s *service) StreamHistoricalSymbolData(ctx context.Context, symbol string) error {

	// stream the historical data over NATS in a goroutine
	go func() {

		// first write all the preexisting historical data for this symbol to
		// the channel
		rows, err := s.DBQ().GetSymbolDataRaw(ctx, dbgen.GetSymbolDataRawParams{
			Symregexp: symbol,
			TsStart:   pgtype.Timestamptz{Time: time.Now().Add(-time.Hour * 24), Valid: true},
			TsEnd:     pgtype.Timestamptz{Time: time.Now(), Valid: true},
		})
		if err != nil {
			s.Log(int(slog.LevelError), "Failed to get historical data", "symbol", symbol, "error", err)
			return
		}

		histData := []api.FeedCompactQuote{}
		for _, row := range rows {
			histData = append(histData, api.FeedCompactQuote{
				FeedCompactQuote: dx.FeedCompactQuote{
					AskPrice:    float64(row.AskPrice),
					AskSize:     float64(row.AskSize),
					BidPrice:    float64(row.BidPrice),
					BidSize:     float64(row.BidSize),
					EventSymbol: row.Symbol,
					EventType:   "QUOTE",
				},
			})
		}

		b, err := json.Marshal(histData)
		if err != nil {
			s.Log(int(slog.LevelError), "Failed to marshal historical data", "symbol", symbol, "error", err)
		}
		// maybe clear the stream?
		s.PublishSymbolData(b)
	}()

	return nil
}

// StartSymbolStream starts a new stream for the given symbol.
// It returns an error if the stream cannot be started (in the future
// I expect this will happen because we'll saturate our TastyTrade API
// symbol subscription limit, but at that point we can just return a
// response that tells the client to try again later (and maybe they'll
// get a server that can handle more symbol subscriptions). Note that because
// this publishes symbol data to the NATS server, it should only be called
// once per symbol, so if have some other publisher publishing on the same
// symbol, you may encounter unexpected behavior.
func (s *service) StartSymbolStream(symbol string) error {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	s.streams[symbol] = cancel

	// start the stream in a goroutine
	go func() {
		defer s.StopSymbolStream(symbol)

		// get a feed channel and stream it
		feeds, err := s.StreamAPIFeedCompactQuoteData(ctx, []string{symbol})
		if err != nil {
			s.Log(int(slog.LevelError), "Failed to start symbol stream", "symbol", symbol, "error", err)
			return
		}

		for {
			select {
			case <-ctx.Done():
				return
			case feed, ok := <-feeds:
				if !ok {
					return
				}
				b, err := json.Marshal(feed)
				if err != nil {
					s.Log(int(slog.LevelError), "Failed to marshal feed", "symbol", symbol, "error", err)
					continue
				}
				s.PublishSymbolData(b)
			}
		}
	}()
	return nil
}

// Add a method to stop streaming a symbol. If the symbol is not being streamed,
// this method is a no-op.
func (s *service) StopSymbolStream(symbol string) {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()

	if cancel, exists := s.streams[symbol]; exists {
		cancel() // Cancel the context
		delete(s.streams, symbol)
	}
}

// Add a method to stop all streams
func (s *service) StopAllStreams() {
	s.streamsMu.Lock()
	defer s.streamsMu.Unlock()

	for symbol, cancel := range s.streams {
		cancel()
		delete(s.streams, symbol)
	}
}

// IsSymbolStreamActive returns true if the symbol is currently being streamed
func (s *service) IsSymbolStreamActive(symbol string) bool {
	s.streamsMu.RLock()
	defer s.streamsMu.RUnlock()
	_, exists := s.streams[symbol]
	return exists
}
