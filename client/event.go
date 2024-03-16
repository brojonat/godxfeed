package client

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

const EVENT_TYPE_MESSAGE = "Message"
const EVENT_TYPE_QUOTE = "Quote"
const EVENT_TYPE_PROFILE = "Profile"
const EVENT_TYPE_GREEKS = "Greeks"
const EVENT_TYPE_SUMMARY = "Summary"
const EVENT_TYPE_CANDLE = "Candle"
const EVENT_TYPE_THEO_PRICE = "TheoPrice"
const EVENT_TYPE_UNDERLYING = "Underlying"
const EVENT_TYPE_CONFIGURATION = "Configuration"
const EVENT_TYPE_OPTION_SALE = "OptionSale"
const EVENT_TYPE_SERIES = "Series"
const EVENT_TYPE_TIME_AND_SALE = "TimeAndSale"
const EVENT_TYPE_TRADE = "Trade"
const EVENT_TYPE_TRADE_ETH = "TradeETH"
const EVENT_TYPE_ORDER = "Order"
const EVENT_TYPE_SPREAD_ORDER = "SpreadOrder"
const EVENT_TYPE_ANALYTIC_ORDER = "AnalyticOrder"

func GetFeedEvents() []string {
	return []string{
		EVENT_TYPE_MESSAGE,
		EVENT_TYPE_QUOTE,
		EVENT_TYPE_PROFILE,
		EVENT_TYPE_GREEKS,
		EVENT_TYPE_SUMMARY,
		EVENT_TYPE_CANDLE,
		EVENT_TYPE_THEO_PRICE,
		EVENT_TYPE_UNDERLYING,
		EVENT_TYPE_CONFIGURATION,
		EVENT_TYPE_OPTION_SALE,
		EVENT_TYPE_SERIES,
		EVENT_TYPE_TIME_AND_SALE,
		EVENT_TYPE_TRADE,
		EVENT_TYPE_TRADE_ETH,
		EVENT_TYPE_ORDER,
		EVENT_TYPE_SPREAD_ORDER,
		EVENT_TYPE_ANALYTIC_ORDER,
	}
}

type EventMessage struct {
	EventType   string          `json:"eventType"`
	EventSymbol string          `json:"eventSymbol"`
	EventTime   int64           `json:"eventTime"`
	Attachment  json.RawMessage `json:"attachment"`
}

type EventQuote struct {
	EventType       string          `json:"eventType"`
	Sequence        int64           `json:"sequence"`
	TimeNanoPart    int64           `json:"timeNanoPart"`
	BidTime         int64           `json:"bidTime"`
	BidExchangeCode string          `json:"bidExchangeCode"`
	BidPrice        decimal.Decimal `json:"bidPrice"`
	BidSize         decimal.Decimal `json:"bidSize"`
	AskTime         int64           `json:"askTime"`
	AskExchangeCode string          `json:"askExchangeCode"`
	AskPrice        decimal.Decimal `json:"askPrice"`
	AskSize         decimal.Decimal `json:"askSize"`
	EventSymbol     string          `json:"eventSymbol"`
	EventTime       int64           `json:"eventTime"`
}

type EventProfile struct {
	EventType            string          `json:"eventType"`
	Description          string          `json:"description"`
	ShortSaleRestriction string          `json:"shortSaleRestriction"`
	TradingStatus        string          `json:"tradingStatus"`
	StatusReason         string          `json:"statusReason"`
	HaltStartTime        int64           `json:"haltStartTime"`
	HaltEndTime          int64           `json:"haltEndTime"`
	HighLimitPrice       decimal.Decimal `json:"highLimitPrice"`
	LowLimitPrice        decimal.Decimal `json:"lowLimitPrice"`
	High52WeekPrice      decimal.Decimal `json:"high52WeekPrice"`
	Low52WeekPrice       decimal.Decimal `json:"low52WeekPrice"`
	Beta                 decimal.Decimal `json:"beta"`
	EarningsPerShare     decimal.Decimal `json:"earningsPerShare"`
	DividendFrequency    decimal.Decimal `json:"dividendFrequency"`
	ExDividendAmount     decimal.Decimal `json:"exDividendAmount"`
	ExDividendDayID      int64           `json:"exDividendDayId"` // FIXME: checking this, maybe?
	Shares               int64           `json:"shares"`
	FreeFloat            decimal.Decimal `json:"freeFloat"`
	EventSymbol          string          `json:"eventSymbol"`
	EventTime            int64           `json:"eventTime"`
}

type EventGreeks struct {
	EventType   string          `json:"eventType"`
	EventFlags  int64           `json:"eventFlags"`
	Index       int64           `json:"index"`
	Time        int64           `json:"time"`
	Sequence    int64           `json:"sequence"`
	Price       decimal.Decimal `json:"price"`
	Volatility  decimal.Decimal `json:"volatility"`
	Delta       decimal.Decimal `json:"delta"`
	Gamma       decimal.Decimal `json:"gamma"`
	Theta       decimal.Decimal `json:"theta"`
	Rho         decimal.Decimal `json:"rho"`
	Vega        decimal.Decimal `json:"vega"`
	EventSymbol string          `json:"eventSymbol"`
	EventTime   int64           `json:"eventTime"`
}

type EventSummary struct {
	EventType             string          `json:"eventType"`
	DayID                 int64           `json:"dayId"`
	DayOpenPrice          decimal.Decimal `json:"dayOpenPrice"`
	DayHighPrice          decimal.Decimal `json:"dayHighPrice"`
	DayLowPrice           decimal.Decimal `json:"dayLowPrice"`
	DayClosePrice         decimal.Decimal `json:"dayClosePrice"`
	DayClosePriceType     string          `json:"dayClosePriceType"`
	PrevDayID             int64           `json:"prevDayId"`
	PrevDayClosePrice     decimal.Decimal `json:"prevDayClosePrice"`
	PrevDayClosePriceType string          `json:"prevDayClosePriceType"`
	PrevDayVolume         int64           `json:"prevDayVolume"`
	OpenInterest          int64           `json:"openInterest"`
	EventSymbol           string          `json:"eventSymbol"`
	EventTime             int64           `json:"eventTime"`
}

type EventCandle struct {
	EventType     string          `json:"eventType"`
	EventSymbol   string          `json:"eventSymbol"`
	EventTime     int64           `json:"eventTime"`
	EventFlags    int64           `json:"eventFlags"`
	Index         int64           `json:"index"`
	Time          int64           `json:"time"`
	Sequence      int64           `json:"sequence"`
	Count         int64           `json:"count"`
	Open          decimal.Decimal `json:"open"`
	High          decimal.Decimal `json:"high"`
	Low           decimal.Decimal `json:"low"`
	Close         decimal.Decimal `json:"close"`
	Volume        decimal.Decimal `json:"volume"`
	VWAP          decimal.Decimal `json:"VWAP"`
	BidVolume     int64           `json:"bidVolume"`
	AskVolume     int64           `json:"askVolume"`
	ImpVolatility decimal.Decimal `json:"impVolatility"`
	OpenInterest  int64           `json:"openInterest"`
}

type EventTheoPrice struct {
	EventType       string          `json:"eventType"`
	EventFlags      int64           `json:"eventFlags"`
	Index           int64           `json:"index"`
	Time            int64           `json:"time"`
	Sequence        int64           `json:"sequence"`
	Price           decimal.Decimal `json:"price"`
	UnderlyingPrice decimal.Decimal `json:"underlyingPrice"`
	Delta           decimal.Decimal `json:"delta"`
	Gamma           decimal.Decimal `json:"gamma"`
	Dividend        decimal.Decimal `json:"dividend"`
	Interest        decimal.Decimal `json:"interest"`
	EventSymbol     string          `json:"eventSymbol"`
	EventTime       int64           `json:"eventTime"`
}

type EventUnderlying struct {
	EventType       string          `json:"eventType"`
	EventFlags      int64           `json:"eventFlags"`
	Index           int64           `json:"index"`
	Time            int64           `json:"time"`
	Sequence        int64           `json:"sequence"`
	Volatility      decimal.Decimal `json:"volatility"`
	FrontVolatility decimal.Decimal `json:"frontVolatility"`
	BackVolatility  decimal.Decimal `json:"backVolatility"`
	CallVolume      int64           `json:"callVolume"`
	PutVolume       int64           `json:"putVolume"`
	PutCallRatio    decimal.Decimal `json:"putCallRatio"`
	EventSymbol     string          `json:"eventSymbol"`
	EventTime       int64           `json:"eventTime"`
}

type EventConfiguration struct {
	EventType   string          `json:"eventType"`
	EventSymbol string          `json:"eventSymbol"`
	EventTime   int64           `json:"eventTime"`
	Version     int64           `json:"version"`
	Attachment  json.RawMessage `json:"attachment"`
}

type EventOptionSale struct {
	EventType              string          `json:"eventType"`
	EventFlags             int64           `json:"eventFlags"`
	Index                  int64           `json:"index"`
	Time                   int64           `json:"time"`
	TimeNanoPart           int64           `json:"timeNanoPart"`
	Sequence               int64           `json:"sequence"`
	ExchangeCode           string          `json:"exchangeCode"`
	Price                  decimal.Decimal `json:"price"`
	Size                   decimal.Decimal `json:"size"`
	BidPrice               decimal.Decimal `json:"bidPrice"`
	AskPrice               decimal.Decimal `json:"askPrice"`
	ExchangeSaleConditions string          `json:"exchangeSaleConditions"`
	TradeThroughExempt     string          `json:"tradeThroughExempt"`
	AggressorSide          string          `json:"aggressorSide"`
	SpreadLeg              bool            `json:"spreadLeg"`
	ExtendedTradingHours   bool            `json:"extendedTradingHours"`
	ValidTick              bool            `json:"validTick"`
	Type                   string          `json:"type"`
	UnderlyingPrice        decimal.Decimal `json:"underlyingPrice"`
	Volatility             decimal.Decimal `json:"volatility"`
	Delta                  decimal.Decimal `json:"delta"`
	OptionSymbol           string          `json:"optionSymbol"`
	EventSymbol            string          `json:"eventSymbol"`
	EventTime              int64           `json:"eventTime"`
}

type EventSeries struct {
	EventType    string          `json:"eventType"`
	EventFlags   int64           `json:"eventFlags"`
	Index        int64           `json:"index"`
	Time         int64           `json:"time"`
	Sequence     int64           `json:"sequence"`
	Expiration   int64           `json:"expiration"`
	Volatility   decimal.Decimal `json:"volatility"`
	CallVolume   int64           `json:"callVolume"`
	PutVolume    int64           `json:"putVolume"`
	PutCallRatio decimal.Decimal `json:"putCallRatio"`
	ForwardPrice decimal.Decimal `json:"forwardPrice"`
	Dividend     decimal.Decimal `json:"dividend"`
	Interest     decimal.Decimal `json:"interest"`
	EventSymbol  string          `json:"eventSymbol"`
	EventTime    int64           `json:"eventTime"`
}

type EventTimeAndSale struct {
	EventType              string          `json:"eventType"`
	EventFlags             int64           `json:"eventFlags"`
	Index                  int64           `json:"index"`
	Time                   int64           `json:"time"`
	TimeNanoPart           int64           `json:"timeNanoPart"`
	Sequence               int64           `json:"sequence"`
	ExchangeCode           string          `json:"exchangeCode"`
	Price                  decimal.Decimal `json:"price"`
	Size                   decimal.Decimal `json:"size"`
	BidPrice               decimal.Decimal `json:"bidPrice"`
	AskPrice               decimal.Decimal `json:"askPrice"`
	ExchangeSaleConditions string          `json:"exchangeSaleConditions"`
	TradeThroughExempt     string          `json:"tradeThroughExempt"`
	AggressorSide          string          `json:"aggressorSide"`
	SpreadLeg              bool            `json:"spreadLeg"`
	ExtendedTradingHours   bool            `json:"extendedTradingHours"`
	ValidTick              bool            `json:"validTick"`
	Type                   string          `json:"type"`
	Buyer                  string          `json:"buyer"`
	Seller                 string          `json:"seller"`
	EventSymbol            string          `json:"eventSymbol"`
	EventTime              int64           `json:"eventTime"`
}

type EventTrade struct {
	EventType            string          `json:"eventType"`
	Time                 int64           `json:"time"`
	TimeNanoPart         int64           `json:"timeNanoPart"`
	Sequence             int64           `json:"sequence"`
	ExchangeCode         string          `json:"exchangeCode"`
	Price                decimal.Decimal `json:"price"`
	Change               decimal.Decimal `json:"change"`
	Size                 decimal.Decimal `json:"size"`
	DayID                int64           `json:"dayId"`
	DayVolume            int64           `json:"dayVolume"`
	DayTurnover          int64           `json:"dayTurnover"`
	TickDirection        string          `json:"tickDirection"`
	ExtendedTradingHours bool            `json:"extendedTradingHours"`
	EventSymbol          string          `json:"eventSymbol"`
	EventTime            int64           `json:"eventTime"`
}

type EventTradeETH struct {
	EventType            string          `json:"eventType"`
	Time                 int64           `json:"time"`
	TimeNanoPart         int64           `json:"timeNanoPart"`
	Sequence             int64           `json:"sequence"`
	ExchangeCode         string          `json:"exchangeCode"`
	Price                decimal.Decimal `json:"price"`
	Change               decimal.Decimal `json:"change"`
	Size                 decimal.Decimal `json:"size"`
	DayID                int64           `json:"dayId"`
	DayVolume            int64           `json:"dayVolume"`
	DayTurnover          int64           `json:"dayTurnover"`
	TickDirection        string          `json:"tickDirection"`
	ExtendedTradingHours bool            `json:"extendedTradingHours"`
	EventSymbol          string          `json:"eventSymbol"`
	EventTime            int64           `json:"eventTime"`
}

type EventOrder struct {
	EventType    string          `json:"eventType"`
	MarketMaker  string          `json:"marketMaker"`
	EventFlags   int64           `json:"eventFlags"`
	Index        int64           `json:"index"`
	Time         int64           `json:"time"`
	TimeNanoPart int64           `json:"timeNanoPart"`
	Sequence     int64           `json:"sequence"`
	Source       string          `json:"source"`
	Action       string          `json:"action"`
	ActionTime   int64           `json:"actionTime"`
	OrderID      int64           `json:"orderId"`
	AuxOrderID   int64           `json:"auxOrderId"`
	Price        decimal.Decimal `json:"price"`
	Size         decimal.Decimal `json:"size"`
	ExecutedSize decimal.Decimal `json:"executedSize"`
	Count        int64           `json:"count"`
	ExchangeCode string          `json:"exchangeCode"`
	OrderSide    string          `json:"orderSide"`
	Scope        string          `json:"scope"`
	TradeID      int64           `json:"tradeId"`
	TradePrice   decimal.Decimal `json:"tradePrice"`
	TradeSize    decimal.Decimal `json:"tradeSize"`
	EventSymbol  string          `json:"eventSymbol"`
	EventTime    int64           `json:"eventTime"`
}

type EventSpreadOrder struct {
	EventType    string          `json:"eventType"`
	SpreadSymbol string          `json:"spreadSymbol"`
	EventFlags   int64           `json:"eventFlags"`
	Index        int64           `json:"index"`
	Time         int64           `json:"time"`
	TimeNanoPart int64           `json:"timeNanoPart"`
	Sequence     int64           `json:"sequence"`
	Source       string          `json:"source"`
	Action       string          `json:"action"`
	ActionTime   int64           `json:"actionTime"`
	OrderID      int64           `json:"orderId"`
	AuxOrderID   int64           `json:"auxOrderId"`
	Price        decimal.Decimal `json:"price"`
	Size         decimal.Decimal `json:"size"`
	ExecutedSize decimal.Decimal `json:"executedSize"`
	Count        int64           `json:"count"`
	ExchangeCode string          `json:"exchangeCode"`
	OrderSide    string          `json:"orderSide"`
	Scope        string          `json:"scope"`
	TradeID      int64           `json:"tradeId"`
	TradePrice   decimal.Decimal `json:"tradePrice"`
	TradeSize    decimal.Decimal `json:"tradeSize"`
	EventSymbol  string          `json:"eventSymbol"`
	EventTime    int64           `json:"eventTime"`
}

type EventAnalyticOrder struct {
	EventType           string          `json:"eventType"`
	IcebergPeakSize     decimal.Decimal `json:"icebergPeakSize"`
	IcebergHiddenSize   decimal.Decimal `json:"icebergHiddenSize"`
	IcebergExecutedSize decimal.Decimal `json:"icebergExecutedSize"`
	IcebergType         string          `json:"icebergType"`
	MarketMaker         string          `json:"marketMaker"`
	EventFlags          int64           `json:"eventFlags"`
	Index               int64           `json:"index"`
	Time                int64           `json:"time"`
	TimeNanoPart        int64           `json:"timeNanoPart"`
	Sequence            int64           `json:"sequence"`
	Source              string          `json:"source"`
	Action              string          `json:"action"`
	ActionTime          int64           `json:"actionTime"`
	OrderID             int64           `json:"orderId"`
	AuxOrderID          int64           `json:"auxOrderId"`
	Price               decimal.Decimal `json:"price"`
	Size                decimal.Decimal `json:"size"`
	ExecutedSize        decimal.Decimal `json:"executedSize"`
	Count               int64           `json:"count"`
	ExchangeCode        string          `json:"exchangeCode"`
	OrderSide           string          `json:"orderSide"`
	Scope               string          `json:"scope"`
	TradeID             int64           `json:"tradeId"`
	TradePrice          int64           `json:"tradePrice"`
	TradeSize           int64           `json:"tradeSize"`
	EventSymbol         string          `json:"eventSymbol"`
	EventTime           int64           `json:"eventTime"`
}
