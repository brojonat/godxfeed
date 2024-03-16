package client

import "encoding/json"

// Message types
const MESSAGE_TYPE_ERROR = "ERROR"
const MESSAGE_TYPE_SETUP = "SETUP"
const MESSAGE_TYPE_KEEPALIVE = "KEEPALIVE"
const MESSAGE_TYPE_AUTH = "AUTH"
const MESSAGE_TYPE_AUTH_STATE = "AUTH_STATE"
const MESSAGE_TYPE_CHANNEL_REQUEST = "CHANNEL_OPEN"
const MESSAGE_TYPE_CHANNEL_CANCEL = "CHANNEL_CANCEL"
const MESSAGE_TYPE_CHANNEL_OPENED = "CHANNEL_OPENED"
const MESSAGE_TYPE_CHANNEL_CLOSED = "CHANNEL_CLOSED"
const MESSAGE_TYPE_FEED_SETUP = "FEED_SETUP"
const MESSAGE_TYPE_FEED_SUBSCRIPTION = "FEED_SUBSCRIPTION"
const MESSAGE_TYPE_FEED_CONFIG = "FEED_CONFIG"
const MESSAGE_TYPE_FEED_DATA = "FEED_DATA"

// Error types
const ERROR_TYPE_UNSUPPORTED_PROTOCOL = "UNSUPPORTED_PROTOCOL"
const ERROR_TYPE_TIMEOUT = "TIMEOUT"
const ERROR_TYPE_UNAUTHORIZED = "UNAUTHORIZED"
const ERROR_TYPE_INVALID_MESSAGE = "INVALID_MESSAGE"
const ERROR_TYPE_BAD_ACTION = "BAD_ACTION"
const ERROR_TYPE_UNKNOWN = "UNKNOWN"

// Auth states
const AUTH_STATE_AUTHORIZED = "AUTHORIZED"
const AUTH_STATE_UNAUTHORIZED = "UNAUTHORIZED"

// Channel services
const CHANNEL_SERVICE_FEED = "FEED"

// Message is a simple interface that implements MarshalJSON
type Message interface {
	MarshalJSON() ([]byte, error)
}

type MessageBase struct {
	Type    string `json:"message_type"`
	Channel string `json:"channel_id"`
}

func (m *MessageBase) Marshal() ([]byte, error) {
	return json.Marshal(m)
}

// Receiving an error message does not require any action and is for
// informational purposes only.
type MessageError struct {
	MessageBase
	Error   string `json:"error"`
	Message string `json:"message"`
}

// The first message the client must send to the server after a successful
// connection.
type MessageSetup struct {
	MessageBase
	KeepAliveTimeout       int    `json:"keepaliveTimeout"`
	AcceptKeepAliveTimeout int    `json:"acceptKeepaliveTimeout"`
	Version                string `json:"version"`
}

// The client must send any messages to the server at least once before the
// timeout is reached (see `SETUP` message). Otherwise, the server will close
// the connection.
//
// The server must send any messages to the client at least once before the
// timeout is reached (see `SETUP` message). Otherwise, the client should close
// the connection.
type MessageKeepalive struct {
	MessageBase
}

// The client must send this message to the server after the connection is
// established and the `SETUP` is sent.
//
// The server will notify with the `AUTH_STATE` message or close the connection
// if the authorization is unsuccessful.
type MessageAuth struct {
	MessageBase
	Token string `json:"token"`
}

// The server will send this message with an `UNAUTHORIZED state` to notify that
// authorization is required.
//
// The server will send this message with an `AUTHORIZED state` to notify that
// authorization was successful or not required.
type MessageAuthState struct {
	MessageBase
	State string `json:"state"`
}

// The client can send this message to the server to open a channel for two-way
// communication with the service.
//
// The server will notify the client with the `CHANNEL_OPENED` message.
type MessageChannelRequest struct {
	MessageBase
	Service    string       `json:"service"`
	Parameters FeedContract `json:"parameters"`
}

// The client can send this message to the server to close the channel.
//
// Once the client has sent this message, the client can forget about the
// channel.
type MessageChannelCancel struct {
	MessageBase
}

// The server will send this message to the client after the `CHANNEL_REQUEST`
// message is received.
type MessageChannelOpened struct {
	MessageBase
	Service    string       `json:"service"`
	Parameters FeedContract `json:"parameters"`
}

// The server will send this message to the client after the `CHANNEL_CANCEL`
// message is received or if the server decides to close the channel.
type MessageChannelClosed struct {
	MessageBase
}

// The client can send this message to the server after opening a channel with
// the `FEED` service.
// The server will notify the client of the application with the `FEED_CONFIG`
// message.
type MessageFeedSetup struct {
	MessageBase
	AcceptAggregationPeriod int             `json:"acceptAggregationPeriod"`
	AcceptEventFields       FeedEventFields `json:"acceptEventFields"`
	AcceptDataFormat        string          `json:"acceptDataFormat"`
}

// The client can send this message to the server after opening a channel with
// the `FEED` service.
type FeedSubscriptionMessage[FS FeedSubscription] struct {
	MessageBase
	Add    []FS `json:"add"`
	Remove []FS `json:"remove"`
	Reset  bool `json:"reset"`
}

// The server can send this message to the client after receiving the
// `FEED_CONFIG` message.
//
// The server can send this message to the client if the `FEED` service
// configuration has changed.
//
// Parameters are lazy therefore the server may not send the notification
// immediately, but before the first `FEED_DATA` is sent.
type FeedConfigMessage struct {
	MessageBase
	AggregationPeriod int             `json:"aggregationPeriod"`
	EventFields       FeedEventFields `json:"eventFields"`
	DataFormat        string          `json:"dataFormat"`
}

// The server can send this message to the client after receiving the
// `FEED_SUBSCRIPTION` message.
//
// The server will send data in the format that was described in the
// `FEED_CONFIG` message.
type FeedDataMessage struct {
	MessageBase
	// structure depends on full or compact mode
	Data json.RawMessage `json:"data"`
}
