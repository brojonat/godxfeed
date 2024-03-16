package client

import "net"

type ClientError struct {
	Message string `json:"message"`
}

func (ce *ClientError) Error() string {
	return ce.Message
}

// example implementaiton of a Client
type BasicClient struct {
	conn      *net.Conn
	authToken string
	handlers  []func(Message) error
	messages  chan Message
}

func (bc *BasicClient) Dial(a string) error {
	return nil
}

func (bc *BasicClient) SetAuthToken(t string) error {
	bc.authToken = t
	return nil
}

func (bc *BasicClient) OpenChannel(cfg ChannelConfig) error {
	// opens a channel
	return nil
}

func (bc *BasicClient) Send(m Message) error {
	// sends a websocket message
	return nil
}

func (bc *BasicClient) AddSubscription(s Subscription) error {
	// adds a subscription by sending a websocket message
	return nil
}

func (bc *BasicClient) RemoveSubscription(s Subscription) error {
	// adds a subscription by sending a websocket message
	return nil
}

func (bc *BasicClient) AddMessageHandler(name string, h func(Message) error) {
	bc.handlers = append(bc.handlers, h)
}

func (bc *BasicClient) Run() error {
	// for m := range bc.messages {
	// 	for h := range bc.handlers {
	// 		h(m)
	// 	}
	// }
	return &ClientError{Message: "client message source closed"}
}

// Dial(string) error
// SetAuthToken(string) error
// OpenChannel(ChannelConfig) error
// Send(Message) error
// AddSubscription(Subscription) error
// RemoveSubscription(Subscription) error
// AddMessageHandler(string, func(Message) error)
// RemoveMessageHandler(string)
// Run() error
