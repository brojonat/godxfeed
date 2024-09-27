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

type Client interface {
	// Dial sets up the websocket connection to the dxlink service
	Dial(context.Context, string, func(MessageSetup) error) error
	// Authenticate performs authentication
	Authenticate(string) error
	Subscribe([]string) error
	// AddMessageHandler adds a handler that will receive each message
	AddMessageHandler(string, func(Message))
	// RemoveMessageHandler removes the handler specified by the supplied string
	RemoveMessageHandler(string)
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
	c.AddMessageHandler(hid, func(m Message) {
		msg, ok := m.(MessageSetup)
		if !ok {
			return
		}
		// remove handler now that setup is done
		c.RemoveMessageHandler(hid)

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
	c.AddMessageHandler(hid, func(m Message) {
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
		c.RemoveMessageHandler(hid)
		done <- nil
	})

	c.Send(MessageAuth{MessageBase: MessageBase{Type: MESSAGE_TYPE_AUTH, Channel: 0}, Token: token})
	return <-done
}

// Returns an error indicating whether channel subscription successful or not.
func (c *client) openChannel(m MessageChannelRequest) error {
	done := make(chan error)
	hid := fmt.Sprintf("_onChannelRequest-%s", uuid.New())
	c.AddMessageHandler(hid, func(m Message) {
		_, ok := m.(MessageChannelOpened)
		if !ok {
			return
		}
		c.RemoveMessageHandler(hid)
		c.Log(int(slog.LevelDebug), "completed channel open")
		done <- nil
	})
	c.Send(m)
	return <-done
}

func (c *client) getNextChanID() int {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.maxChannelID += 1
	return c.maxChannelID
}

// Returns an error indicating whether channel subscription successful or not.
func (c *client) Subscribe(syms []string) error {
	cid := c.getNextChanID()
	if err := c.openChannel(MessageChannelRequest{
		MessageBase: MessageBase{Type: MESSAGE_TYPE_CHANNEL_REQUEST, Channel: cid},
		Service:     CHANNEL_SERVICE_FEED,
		Parameters:  FeedContract{Contract: FEED_CONTRACT_STREAM},
	}); err != nil {
		return err
	}

	done := make(chan error)

	hid := fmt.Sprintf("_onFeedSetup-%s", uuid.New())
	c.AddMessageHandler(hid, func(m Message) {
		_, ok := m.(MessageFeedConfig)
		if !ok {
			return
		}
		c.RemoveMessageHandler(hid)
		c.Log(int(slog.LevelDebug), "completed feed setup")
		done <- nil
	})
	c.Send(MessageFeedSetup{
		MessageBase:             MessageBase{Type: MESSAGE_TYPE_FEED_SETUP, Channel: cid},
		AcceptAggregationPeriod: 1.0,
		AcceptEventFields:       map[string][]string{"Quote": {"eventType", "eventSymbol", "bidPrice", "askPrice", "bidSize", "askSize"}},
		AcceptDataFormat:        FEED_DATA_FORMAT_FULL,
	})
	if err := <-done; err != nil {
		return err
	}

	hid = fmt.Sprintf("_onFeedSubscription-%s", uuid.New())
	c.AddMessageHandler(hid, func(m Message) {
		_, ok := m.(MessageFeedConfig)
		if !ok {
			return
		}
		c.RemoveMessageHandler(hid)
		c.Log(int(slog.LevelDebug), "completed feed subscription")
		done <- nil
	})

	// FIXME: chunk this into 50ish symbols at a time
	subs := []FeedRegularSubscription{}
	for _, sym := range syms {
		subs = append(subs, FeedRegularSubscription{Type: "Quote", Symbol: FeedSymbol(sym)})
	}

	c.Send(MessageFeedRegularSubscription{
		MessageBase: MessageBase{Type: MESSAGE_TYPE_FEED_SUBSCRIPTION, Channel: cid},
		Add:         subs,
		Remove:      []FeedRegularSubscription{},
		Reset:       false,
	})
	return <-done
}

func (c *client) C() (<-chan Message, string) {
	out := make(chan Message)
	hid := fmt.Sprintf("streamer-%s", uuid.New())
	c.AddMessageHandler(hid, func(m Message) {
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

func (c *client) AddMessageHandler(name string, f func(Message)) {
	c.lock.Lock()
	defer c.lock.Unlock()
	c.handlers[name] = f
}

func (c *client) RemoveMessageHandler(name string) {
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
