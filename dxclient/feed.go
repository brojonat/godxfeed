package dxclient

import "encoding/json"

const FEED_CONTRACT_HISTORY = "HISTORY"
const FEED_CONTRACT_TICKER = "TICKER"
const FEED_CONTRACT_STREAM = "STREAM"
const FEED_CONTRACT_AUTO = "AUTO"

const FEED_DATA_FORMAT_FULL = "FULL"
const FEED_DATA_FORMAT_COMPACT = "COMPACT"

// Subscription contract of the `FEED` service. Possible values:
// - `HISTORY` - real-time history of events with snapshots (like Candles)
// - `TICKER` - real-time ticker of events (like Quotes)
// - `STREAM` - real-time stream of events (like Orders)
// - `AUTO` - automatic selection of contract type depending on event type:
//   - `HISTORY` - for `Candle` event type and others
//   - `TICKER` - for `Quote` event type and others
//   - `STREAM` - for `Order` event type and others
type FeedContract struct {
	Contract string `json:"contract"`
}

// An object where keys are event types and values are an array of event fields.
type FeedEventFields struct {
	PropertyNames        string   `json:"propertyNames"`
	AdditionalProperties []string `json:"additionalProperties"`
}

// Symbol of the subscription.
// - `*` - wildcard (all symbols) subscription (for `STREAM` and `AUTO` contracts only)
// - `symbol` - one symbol
type FeedSymbol string

// This type of subscription object is used in a channel with `TICKER`,
// `STREAM`, or `AUTO` contract.
type FeedRegularSubscription struct {
	Type   string     `json:"type"`
	Symbol FeedSymbol `json:"symbol"`
}

func (m FeedRegularSubscription) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// This type of subscription object is used in a channel with `HISTORY` or
// `AUTO` contract.
type FeedOrderBookSubscription struct {
	Type   string     `json:"type"`
	Symbol FeedSymbol `json:"symbol"`
	Source string     `json:"source"`
}

func (m FeedOrderBookSubscription) JSON() ([]byte, error) {
	return json.Marshal(m)
}

// This type of subscription object is used in a channel with `HISTORY` or
// `AUTO` contract.
type FeedTimeSeriesSubscription struct {
	Type     string     `json:"type"`
	Symbol   FeedSymbol `json:"symbol"`
	FromTime int64      `json:"fromTime"`
}

func (m FeedTimeSeriesSubscription) JSON() ([]byte, error) {
	return json.Marshal(m)
}
