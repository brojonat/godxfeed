package tastytrade

import "encoding/json"

type Response struct {
	Data    json.RawMessage `json:"data,omitempty"`
	Error   Error           `json:"error,omitempty"`
	Context string          `json:"context,omitempty"`
}

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type TokenData struct {
	Token     string `json:"token"`
	DXLinkURL string `json:"dxlink-url"`
	Level     string `json:"level"`
}

type EquitySymbol struct {
	ID                             int             `json:"id"`
	Symbol                         string          `json:"symbol"`
	InstrumentType                 string          `json:"instrument-type"`
	CUSIP                          string          `json:"cusip"`
	ShortDescription               string          `json:"short-description"`
	IsIndex                        bool            `json:"is-index"`
	ListedMarket                   string          `json:"listed-market"`
	Description                    string          `json:"description"`
	Lendability                    string          `json:"lendability"`
	BorrowRate                     string          `json:"borrow-rate"`
	MarketTimeInstrumentCollection string          `json:"market-time-instrument-collection"`
	IsClosingOnly                  bool            `json:"is-closing-only"`
	IsOptionsClosingOnly           bool            `json:"is-options-closing-only"`
	Active                         bool            `json:"active"`
	IsFractionalQuantityEligible   bool            `json:"is-fractional-quantity-eligible"`
	IsIlliquid                     bool            `json:"is-illiquid"`
	IsETF                          bool            `json:"is-etf"`
	IsFraudRisk                    bool            `json:"is-fraud-risk"`
	StreamerSymbol                 string          `json:"streamer-symbol"`
	TickSizes                      json.RawMessage `json:"tick-sizes"`
	OptionTickSizes                json.RawMessage `json:"option-tick-sizes"`
}

type OptionSymbol struct {
	Symbol                         string `json:"symbol"`
	InstrumentType                 string `json:"instrument-type"`
	Active                         bool   `json:"active"`
	StrikePrice                    string `json:"strike-price"`
	RootSymbol                     string `json:"root-symbol"`
	UnderlyingSymbol               string `json:"underlying-symbol"`
	ExpirationDate                 string `json:"expiration-date"`
	ExerciseStyle                  string `json:"exercise-style"`
	SharesPerContract              int    `json:"shares-per-contract"`
	OptionType                     string `json:"option"`
	OptionChainType                string `json:"option-chain-type"`
	ExpirationType                 string `json:"expiration-type"`
	SettlementType                 string `json:"settlement-type"`
	StopsTradingAt                 string `json:"stops-trading-at"`
	MarketTimeInstrumentCollection string `json:"market-time-instrument-collection"`
	DaysToExpiration               int    `json:"days-to-expiration"`
	ExpiresAt                      string `json:"expires-at"`
	IsClosingOnly                  bool   `json:"is-closing-only"`
	StreamerSymbol                 string `json:"streamer-symbol"`
}

const CLIENT_MESSAGE_TYPE_SUBSCRIBE = "subscribe"
const CLIENT_MESSAGE_TYPE_UNSUBSCRIBE = "unsubscribe"

type ClientMessage struct {
	Type string          `json:"type"`
	Body json.RawMessage `json:"body"`
}
