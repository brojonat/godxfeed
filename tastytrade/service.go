package tastytrade

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"sync"

	dx "github.com/brojonat/godxfeed/dxclient"
	bwebsocket "github.com/brojonat/websocket"
)

type Service interface {
	RegisterClient(ctx context.Context, cf context.CancelFunc, c bwebsocket.Client)
	UnregisterClient(c bwebsocket.Client)
	HandleClientMessage(twEndpoint string, twToken string, dxEndpoint string, dxToken string) []bwebsocket.MessageHandler
	SetLogger(l any) error
	Log(l int, m string, args ...any)

	NewSessionToken(endpoint string, username string, password string) (*Response, error)
	TestSessionToken(endpoint string, sessionToken string) (*Response, error)
	NewStreamerToken(endpoint string, sessionToken string) (*Response, error)
	GetSymbolData(endpoint string, sessionToken string, symbol string, symbolType string) (*Response, error)
	GetOptionChain(endpoint string, sessionToken string, symbol string, nested bool, compact bool) (*Response, error)

	GetRelatedSymbols(endpoint string, sessionToken string, symbol string) ([]string, error)
	SubscribeClient(c bwebsocket.Client, cf context.CancelFunc, symbol string) error
	UnsubscribeClient(c bwebsocket.Client, symbol string) error
	StreamSymbols(ctx context.Context, endpoint string, token string, syms []string) (<-chan []byte, error)
	PrepareMessageForClient(symbols []string, message dx.Message, validators ...func([]string, dx.Message) error) ([]byte, error)
}

type service struct {
	logger  *slog.Logger
	subLock *sync.RWMutex
	subs    map[bwebsocket.Client]map[string]context.CancelFunc
}

func NewService() Service {
	return &service{
		logger:  slog.New(slog.NewJSONHandler(os.Stdout, nil)),
		subLock: &sync.RWMutex{},
		subs:    make(map[bwebsocket.Client]map[string]context.CancelFunc),
	}
}

func (s *service) SetLogger(v any) error {
	l, ok := v.(*slog.Logger)
	if !ok {
		return fmt.Errorf("bad logger value supplied")
	}
	s.logger = l
	return nil
}

func (s *service) Log(level int, msg string, args ...any) {
	_, f, l, ok := runtime.Caller(1)
	if ok {
		args = append(args, "caller_source", fmt.Sprintf("%s %d", f, l))
	}
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

// GetRelatedSymbols returns the equity and options symbols for the supplied equity symbol
// sorted by days to expiry.
func (s *service) GetRelatedSymbols(endpoint, token, symbol string) ([]string, error) {
	// get the equity streamer symbols
	twr, err := s.GetSymbolData(
		endpoint,
		token,
		symbol,
		SYMBOL_TYPE_EQUITIES,
	)
	if err != nil {
		return []string{}, fmt.Errorf("could not get equity symbol data: %w", err)
	}
	var eqData EquitySymbol
	if err := json.Unmarshal(twr.Data, &eqData); err != nil {
		return []string{}, fmt.Errorf("could not unmarshal equity symbol data: %w", err)
	}
	syms := []string{eqData.StreamerSymbol}

	// get the option streamer symbols
	twr, err = s.GetSymbolData(
		endpoint,
		token,
		symbol,
		SYMBOL_TYPE_OPTIONS,
	)
	if err != nil {
		return []string{}, fmt.Errorf("could not get option symbology data: %w", err)
	}
	var optsData struct {
		Items []OptionSymbol `json:"items"`
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
		return nil, fmt.Errorf("%w: %w", ErrorInternal{Message: "internal error"}, err)
	}
	if err := c.Authenticate(token); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrorInternal{Message: "internal error"}, err)
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
