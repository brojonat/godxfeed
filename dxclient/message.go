package dxclient

import (
	"encoding/json"
)

// Message types
const MESSAGE_TYPE_ERROR = "ERROR"
const MESSAGE_TYPE_SETUP = "SETUP"
const MESSAGE_TYPE_KEEPALIVE = "KEEPALIVE"
const MESSAGE_TYPE_AUTH = "AUTH"
const MESSAGE_TYPE_AUTH_STATE = "AUTH_STATE"
const MESSAGE_TYPE_CHANNEL_REQUEST = "CHANNEL_REQUEST"
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
	JSON() ([]byte, error)
}

type MessageBase struct {
	Type    string `json:"type"`
	Channel int    `json:"channel"`
}

func (m MessageBase) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// Receiving an error message does not require any action and is for
// informational purposes only.
type MessageError struct {
	MessageBase
	Error   string `json:"error"`
	Message string `json:"message"`
}

func (m MessageError) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The first message the client must send to the server after a successful
// connection.
type MessageSetup struct {
	MessageBase
	KeepAliveTimeout       int    `json:"keepaliveTimeout"`
	AcceptKeepAliveTimeout int    `json:"acceptKeepaliveTimeout"`
	Version                string `json:"version"`
}

func (m MessageSetup) JSON() ([]byte, error) {
	return json.Marshal(m)
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

func (m MessageKeepalive) JSON() ([]byte, error) {
	return json.Marshal(m)
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

func (m MessageAuth) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The server will send this message with an `UNAUTHORIZED state` to notify that
// authorization is required.
//
// The server will send this message with an `AUTHORIZED state` to notify that
// authorization was successful or not required.
type MessageAuthState struct {
	MessageBase
	State  string `json:"state"`
	UserID string `json:"userId"`
}

func (m MessageAuthState) JSON() ([]byte, error) {
	return json.Marshal(m)
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

func (m MessageChannelRequest) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The client can send this message to the server to close the channel.
//
// Once the client has sent this message, the client can forget about the
// channel.
type MessageChannelCancel struct {
	MessageBase
}

func (m MessageChannelCancel) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The server will send this message to the client after the `CHANNEL_REQUEST`
// message is received.
type MessageChannelOpened struct {
	MessageBase
	Service    string       `json:"service"`
	Parameters FeedContract `json:"parameters"`
}

func (m MessageChannelOpened) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The server will send this message to the client after the `CHANNEL_CANCEL`
// message is received or if the server decides to close the channel.
type MessageChannelClosed struct {
	MessageBase
}

func (m MessageChannelClosed) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The client can send this message to the server after opening a channel with
// the `FEED` service.
// The server will notify the client of the application with the `FEED_CONFIG`
// message.
type MessageFeedSetup struct {
	MessageBase
	AcceptAggregationPeriod float64             `json:"acceptAggregationPeriod"`
	AcceptEventFields       map[string][]string `json:"acceptEventFields"`
	AcceptDataFormat        string              `json:"acceptDataFormat"`
}

func (m MessageFeedSetup) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The client can send this message to the server after opening a channel with
// the `FEED` service.
type MessageFeedRegularSubscription struct {
	MessageBase
	Add    []FeedRegularSubscription `json:"add"`
	Remove []FeedRegularSubscription `json:"remove"`
	Reset  bool                      `json:"reset"`
}

func (m MessageFeedRegularSubscription) JSON() ([]byte, error) {
	return json.Marshal(m)
}

type MessageFeedOrderBookSubscription struct {
	MessageBase
	Add    []FeedOrderBookSubscription `json:"add"`
	Remove []FeedOrderBookSubscription `json:"remove"`
	Reset  bool                        `json:"reset"`
}

func (m MessageFeedOrderBookSubscription) JSON() ([]byte, error) {
	return json.Marshal(m)
}

type MessageFeedTimeSeriesSubscription struct {
	MessageBase
	Add    []FeedTimeSeriesSubscription `json:"add"`
	Remove []FeedTimeSeriesSubscription `json:"remove"`
	Reset  bool                         `json:"reset"`
}

func (m MessageFeedTimeSeriesSubscription) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The server can send this message to the client after receiving the
// `FEED_CONFIG` message.
//
// The server can send this message to the client if the `FEED` service
// configuration has changed.
//
// Parameters are lazy therefore the server may not send the notification
// immediately, but before the first `FEED_DATA` is sent.
type MessageFeedConfig struct {
	MessageBase
	AggregationPeriod float64         `json:"aggregationPeriod"`
	EventFields       FeedEventFields `json:"eventFields"`
	DataFormat        string          `json:"dataFormat"`
}

func (m MessageFeedConfig) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// The server can send this message to the client after receiving the
// `FEED_SUBSCRIPTION` message.
//
// The server will send data in the format that was described in the
// `FEED_CONFIG` message.
type MessageFeedData struct {
	MessageBase
	// structure depends on full or compact mode
	Data json.RawMessage `json:"data"`
}

func (m MessageFeedData) JSON() ([]byte, error) {
	return json.Marshal(m)
}
