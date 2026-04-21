package dxclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type ErrClient struct {
	Message string `json:"message"`
}

func (e ErrClient) Error() string {
	return e.Message
}

type ErrBadToken struct {
	Message string `json:"message"`
}

func (e ErrBadToken) Error() string {
	return e.Message
}

type ErrAlreadySubscribed struct {
	Message string `json:"message"`
}

func (e ErrAlreadySubscribed) Error() string {
	return e.Message
}

type Client interface {
	// Dial sets up the websocket connection to the dxlink service
	Dial(context.Context, string, func(MessageSetup) error) error
	// Authenticate performs authentication
	Authenticate(string) error
	// OpenFeed opens the long-lived FEED service channel and completes the
	// initial FEED_SETUP handshake. Callable at most once per connection.
	// Subsequent subscription changes go through UpdateSubscription on the
	// same channel.
	OpenFeed() error
	// UpdateSubscription sends an incremental FEED_SUBSCRIPTION on the
	// feed channel opened by OpenFeed: add to start receiving, remove to
	// stop. Errors if OpenFeed has not been called.
	UpdateSubscription(add, remove []FeedSub) error
	// Send sends the supplied message to the dxlink service
	Send(Message) error
	// Returns a channel of []byte consumers can listen on for all messages
	C() (<-chan Message, string)
	// Log allows implementors to use their own logging dependencies
	Log(int, string, ...any)
	// Block until done
	Wait()
}

func NewClient(lf func(int, string, ...interface{})) Client {
	return &client{
		lock:     &sync.RWMutex{},
		wg:       &sync.WaitGroup{},
		egress:   make(chan egressPayload),
		handlers: make(map[string]func(Message)),
		logfunc:  lf,
	}
}

type client struct {
	lock         *sync.RWMutex
	wg           *sync.WaitGroup
	maxChannelID int
	conn         *websocket.Conn
	handlers     map[string]func(Message)
	egress       chan egressPayload
	logfunc      func(int, string, ...interface{})

	// feedChannelID is the channel ID of the long-lived FEED service
	// channel opened by OpenFeed. Zero means "no feed opened yet" — valid
	// IDs start at 1 via getNextChanID, so the zero value works as the
	// "unopened" sentinel.
	feedChannelID int
}

// Dial sets up the websocket connection and performs the initial setup/auth
// handshake as required per the dxlink API. Dial blocks until a SETUP_MESSAGE
// is received, at which point a goroutine is started that sends the KEEPALIVE
// messages as required by the dxlink API, and finally the supplied callback
// is run using the response as its argument. An error is returned indicating
// the success of the overall setup operation including the error returned
// by the supplied callback.
func (c *client) Dial(ctx context.Context, addr string, onSetupReply func(MessageSetup) error) error {
	conn, _, err := websocket.DefaultDialer.Dial(addr, nil)
	if err != nil {
		return err
	}
	c.conn = conn

	go c.readForever(ctx)
	go c.writeForever(ctx)
	c.wg.Add(2)

	done := make(chan error)

	hid := fmt.Sprintf("_onSetup-%s", uuid.New())
	c.addMessageHandler(hid, func(m Message) {
		msg, ok := m.(MessageSetup)
		if !ok {
			return
		}
		// remove handler now that setup is done
		c.removeMessageHandler(hid)

		// send a keepalive message a bit less than every keepalive period
		go func() {
			interval := time.Duration(msg.KeepAliveTimeout*4/5) * time.Second
			t := time.NewTicker(interval)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					c.Send(MessageKeepalive{MessageBase: MessageBase{Type: MESSAGE_TYPE_KEEPALIVE, Channel: 0}})
				}
			}
		}()
		done <- onSetupReply(msg)
	})
	c.Send(MessageSetup{
		MessageBase:            MessageBase{Type: MESSAGE_TYPE_SETUP, Channel: 0},
		KeepAliveTimeout:       60,
		AcceptKeepAliveTimeout: 60,
		Version:                "0.1",
	})
	return <-done
}

// Authenticate sends the AUTH_MESSAGE and blocks until receiving an AUTH_STATE_MESSAGE.
// Returns an error indicating whether successful or not.
func (c *client) Authenticate(token string) error {
	done := make(chan error)
	hid := fmt.Sprintf("_onAuth-%s", uuid.New())
	unauthorizedCount := 0
	c.addMessageHandler(hid, func(m Message) {
		msg, ok := m.(MessageAuthState)
		if !ok {
			return
		}

		// NOTE: this is an apparent bug in the API. After you authenticate, the
		// server will send TWO AUTH_STATE messages, the first one will have
		// state UNAUTHORIZED, and the second _should_ have state AUTHORIZED, so
		// only return an error if the second message is also UNAUTHORIZED.
		if msg.State != AUTH_STATE_AUTHORIZED {
			unauthorizedCount += 1
			if unauthorizedCount > 1 {
				done <- ErrBadToken{Message: fmt.Sprintf("bad token: %s", token)}
			}
			return
		}
		// remove handler now that authentication is done
		c.removeMessageHandler(hid)
		done <- nil
	})

	c.Send(MessageAuth{MessageBase: MessageBase{Type: MESSAGE_TYPE_AUTH, Channel: 0}, Token: token})
	return <-done
}

// OpenFeed opens the long-lived FEED service channel and completes the
// FEED_SETUP handshake, blocking until the server's FEED_CONFIG ack
// arrives (per the dxLink spec, FEED_CONFIG is only emitted in response
// to FEED_SETUP, not to subsequent FEED_SUBSCRIPTION frames). The channel
// ID is stashed on the client so subsequent UpdateSubscription calls can
// reuse it. Thread-safe, but calling it twice on the same client is a
// protocol violation and errors.
func (c *client) OpenFeed() error {
	c.lock.Lock()
	if c.feedChannelID != 0 {
		c.lock.Unlock()
		return fmt.Errorf("OpenFeed: feed already open on channel %d", c.feedChannelID)
	}
	cid := c.getNextChanIDLocked()
	c.lock.Unlock()

	if err := c.openChannel(MessageChannelRequest{
		MessageBase: MessageBase{Type: MESSAGE_TYPE_CHANNEL_REQUEST, Channel: cid},
		Service:     CHANNEL_SERVICE_FEED,
		Parameters:  FeedContract{Contract: FEED_CONTRACT_STREAM},
	}); err != nil {
		return fmt.Errorf("OpenFeed: open channel: %w", err)
	}

	done := make(chan struct{}, 1)
	hid := fmt.Sprintf("_onFeedConfig-%s", uuid.New())
	c.lock.Lock()
	c.handlers[hid] = func(m Message) {
		msg, ok := m.(MessageFeedConfig)
		if !ok || msg.Channel != cid {
			return
		}
		c.removeMessageHandler(hid)
		select {
		case done <- struct{}{}:
		default:
		}
	}
	c.lock.Unlock()

	if err := c.Send(MessageFeedSetup{
		MessageBase:             MessageBase{Type: MESSAGE_TYPE_FEED_SETUP, Channel: cid},
		AcceptAggregationPeriod: 1.0,
		// Declare interest in all event types the project currently cares
		// about. AcceptEventFields is strictly meaningful in COMPACT data
		// mode; in FULL (what we use) the server sends every field. Listing
		// the event types explicitly still tells the server which streams
		// the client wants to open, so a later FEED_SUBSCRIPTION with
		// `type: "Greeks"` isn't rejected.
		AcceptEventFields: map[string][]string{
			"Quote":      {"eventType", "eventSymbol", "bidPrice", "askPrice", "bidSize", "askSize"},
			"Greeks":     {"eventType", "eventSymbol", "price", "volatility", "delta", "gamma", "theta", "rho", "vega"},
			"TheoPrice":  {"eventType", "eventSymbol", "price", "underlyingPrice", "delta", "gamma", "dividend", "interest"},
			"Underlying": {"eventType", "eventSymbol", "volatility", "frontVolatility", "backVolatility", "putCallRatio"},
		},
		AcceptDataFormat: FEED_DATA_FORMAT_FULL,
	}); err != nil {
		c.removeMessageHandler(hid)
		return fmt.Errorf("OpenFeed: feed setup: %w", err)
	}

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		c.removeMessageHandler(hid)
		return fmt.Errorf("OpenFeed: timeout waiting for FEED_CONFIG on channel %d", cid)
	}

	c.lock.Lock()
	c.feedChannelID = cid
	c.lock.Unlock()
	return nil
}

// UpdateSubscription sends an incremental FEED_SUBSCRIPTION on the open
// feed channel. Per the dxLink spec, FEED_SUBSCRIPTION is fire-and-forget:
// the server does not emit a FEED_CONFIG or any other ack — the arrival
// of FEED_DATA for the new symbol is the only implicit acknowledgement.
// Returns once the frame has been written to the websocket.
//
// Fails fast if OpenFeed has not been called — otherwise a caller bug
// would silently leak FEED_SUBSCRIPTION onto control channel 0.
func (c *client) UpdateSubscription(add, remove []FeedSub) error {
	c.lock.RLock()
	cid := c.feedChannelID
	c.lock.RUnlock()
	if cid == 0 {
		return fmt.Errorf("UpdateSubscription: feed not open (call OpenFeed first)")
	}

	toWire := func(in []FeedSub) []FeedRegularSubscription {
		out := make([]FeedRegularSubscription, 0, len(in))
		for _, s := range in {
			out = append(out, FeedRegularSubscription{Type: s.Event, Symbol: FeedSymbol(s.Symbol)})
		}
		return out
	}

	return c.Send(MessageFeedRegularSubscription{
		MessageBase: MessageBase{Type: MESSAGE_TYPE_FEED_SUBSCRIPTION, Channel: cid},
		Add:         toWire(add),
		Remove:      toWire(remove),
		Reset:       false,
	})
}

// C returns a channel of messages that are sent to the client.
// The second return value is a string that is the ID of the handler that
// is used to remove the handler from the client. Note that many clients
// can subscribe to the same channel and not interfere with each other
// because each client gets its own unique message handler responsible
// for writing to the channel.
func (c *client) C() (<-chan Message, string) {
	out := make(chan Message)
	hid := fmt.Sprintf("streamer-%s", uuid.New())
	c.addMessageHandler(hid, func(m Message) {
		out <- m
	})
	return out, hid
}

func (c *client) writeForever(ctx context.Context) {
	defer c.wg.Done()
	for {
		select {
		// handle cancellation
		case <-ctx.Done():
			c.conn.WriteMessage(websocket.CloseMessage, nil)
			return
		// handle sending out messages
		case payload, ok := <-c.egress:
			// ok will be false in case the egress channel is closed for any reason
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			// write a message to the connection; return if any error is encountered
			if err := c.conn.WriteMessage(websocket.TextMessage, payload.data); err != nil {
				return
			}
			payload.done <- struct{}{}
		}
	}
}

func (c *client) readForever(ctx context.Context) {
	defer c.wg.Done()
	ingress := make(chan Message)
	errCancel := make(chan error)
	loop := true
	go func() {
		defer c.conn.Close()
		for loop {
			_, b, err := c.conn.ReadMessage()
			if err != nil {
				errCancel <- err
				// FIXME: return? probably not
			}
			c.Log(int(slog.LevelDebug), "read message", "data", string(b))
			// parse the message into MessageBase and extract the type
			var mb MessageBase
			err = json.Unmarshal(b, &mb)
			if err != nil {
				c.Log(
					int(slog.LevelError),
					fmt.Sprintf("error deserializing message from server: %s", err),
					"payload", string(b),
				)
				continue
			}

			// switch over all message types to deserialize in full
			switch mb.Type {
			case MESSAGE_TYPE_ERROR:
				var m MessageError
				if err := json.Unmarshal(b, &m); err != nil {
					c.Log(
						int(slog.LevelError),
						fmt.Sprintf("error deserializing message from server: %v", err),
						"payload", string(b),
					)
					continue
				}
				c.Log(
					int(slog.LevelError),
					fmt.Sprintf("received error from server: %s: %s", m.Error, m.Message),
				)
				ingress <- m
			case MESSAGE_TYPE_KEEPALIVE:
				var m MessageKeepalive
				if err := json.Unmarshal(b, &m); err != nil {
					c.Log(
						int(slog.LevelError),
						fmt.Sprintf("error deserializing message from server: %v", err),
						"payload", string(b),
					)
					continue
				}
				ingress <- m
			case MESSAGE_TYPE_SETUP:
				var m MessageSetup
				if err := json.Unmarshal(b, &m); err != nil {
					c.Log(
						int(slog.LevelError),
						fmt.Sprintf("error deserializing message from server: %v", err),
						"payload", string(b),
					)
					continue
				}
				ingress <- m
			case MESSAGE_TYPE_AUTH_STATE:
				var m MessageAuthState
				if err = json.Unmarshal(b, &m); err != nil {
					c.Log(
						int(slog.LevelError),
						fmt.Sprintf("error deserializing message from server: %v", err),
						"payload", string(b),
					)
					continue
				}
				ingress <- m
			case MESSAGE_TYPE_CHANNEL_OPENED:
				var m MessageChannelOpened
				if err = json.Unmarshal(b, &m); err != nil {
					c.Log(
						int(slog.LevelError),
						fmt.Sprintf("error deserializing message from server: %v", err),
						"payload", string(b),
					)
					continue
				}
				ingress <- m
			case MESSAGE_TYPE_FEED_CONFIG:
				var m MessageFeedConfig
				if err = json.Unmarshal(b, &m); err != nil {
					c.Log(
						int(slog.LevelError),
						fmt.Sprintf("error deserializing message from server: %v", err),
						"payload", string(b),
					)
					continue
				}
				ingress <- m
			case MESSAGE_TYPE_FEED_DATA:
				var m MessageFeedData
				if err = json.Unmarshal(b, &m); err != nil {
					c.Log(
						int(slog.LevelError),
						fmt.Sprintf("error deserializing message from server: %v", err),
						"payload", string(b),
					)
					continue
				}
				ingress <- m
			default:
				c.Log(
					int(slog.LevelError),
					"unrecognized message type from server",
					"payload", string(b))
				continue
			}
		}
	}()

	for loop {
		select {
		case <-ctx.Done():
			c.Log(int(slog.LevelInfo), "read loop context cancelled, shutting down")
			loop = false

		case err := <-errCancel:
			c.Log(int(slog.LevelInfo), "read loop error, shutting down")
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				c.Log(int(slog.LevelError), "read loop encountered unexpected error, shutting down", "error", err.Error())
			}
			loop = false

		case m := <-ingress:
			// copy the current state of the handlers
			c.lock.Lock()
			handlers := make([]func(Message), 0)
			for _, h := range c.handlers {
				handlers = append(handlers, h)
			}
			c.lock.Unlock()
			// run each handler concurrently
			var wg sync.WaitGroup
			wg.Add(len(handlers))
			for _, h := range handlers {
				go func(h func(Message)) {
					h(m)
					wg.Done()
				}(h)
			}
			wg.Wait()
		}
	}

}

func (c *client) addMessageHandler(name string, f func(Message)) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.handlers[name] = f
}

func (c *client) removeMessageHandler(name string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	delete(c.handlers, name)
}

type egressPayload struct {
	done chan struct{}
	data []byte
}

// Send sends the supplied message and blocks until it is sent. It is safe to
// call Send from multiple goroutines.
func (c *client) Send(m Message) error {
	b, err := m.JSON()
	if err != nil {
		return err
	}
	done := make(chan struct{})
	p := egressPayload{
		done: done,
		data: b,
	}
	c.egress <- p
	<-done
	c.Log(int(slog.LevelDebug), "sent data", "data", string(b))
	return nil
}

func (c *client) Log(level int, s string, args ...any) {
	c.logfunc(level, s, args...)
}

// Wait blocks until the client is done reading/writing
func (c *client) Wait() {
	c.wg.Wait()
}

// getNextChanIDLocked allocates a fresh channel ID. Callers must hold
// c.lock — the RWMutex is not reentrant, so a public Lock() + call-through
// to a second Lock() deadlocks.
func (c *client) getNextChanIDLocked() int {
	c.maxChannelID += 1
	return c.maxChannelID
}

func (c *client) openChannel(req MessageChannelRequest) error {
	done := make(chan error)

	c.lock.Lock()
	hid := fmt.Sprintf("_onChannelOpen-%s", uuid.New())
	openHandler := func(m Message) {
		msg, ok := m.(MessageChannelOpened)
		if !ok {
			return
		}
		if msg.Channel != req.Channel {
			return
		}
		c.removeMessageHandler(hid)
		c.Log(int(slog.LevelDebug), "channel opened", "channel", msg.Channel)
		done <- nil
	}
	c.handlers[hid] = openHandler
	c.lock.Unlock()

	if err := c.Send(req); err != nil {
		return fmt.Errorf("failed to send channel request: %w", err)
	}

	return <-done
}
