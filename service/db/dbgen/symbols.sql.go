// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.26.0
// source: symbols.sql

package dbgen

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

const getSymbolDataRaw = `-- name: GetSymbolDataRaw :many
SELECT
    s.symbol AS "symbol",
    s.ts AS "ts",
    s.bid_price::REAL AS "bid_price",
    s.bid_size::REAL AS "bid_size",
    s.ask_price::REAL AS "ask_price",
    s.ask_size::REAL AS "ask_size"
FROM symbol_bid_ask AS s
WHERE
    s.symbol SIMILAR TO $1 AND
    s.ts >= $2 AND
    s.ts <= $3
`

type GetSymbolDataRawParams struct {
	Symregexp string             `json:"symregexp"`
	TsStart   pgtype.Timestamptz `json:"ts_start"`
	TsEnd     pgtype.Timestamptz `json:"ts_end"`
}

type GetSymbolDataRawRow struct {
	Symbol   string             `json:"symbol"`
	Ts       pgtype.Timestamptz `json:"ts"`
	BidPrice float32            `json:"bid_price"`
	BidSize  float32            `json:"bid_size"`
	AskPrice float32            `json:"ask_price"`
	AskSize  float32            `json:"ask_size"`
}

func (q *Queries) GetSymbolDataRaw(ctx context.Context, arg GetSymbolDataRawParams) ([]GetSymbolDataRawRow, error) {
	rows, err := q.db.Query(ctx, getSymbolDataRaw, arg.Symregexp, arg.TsStart, arg.TsEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetSymbolDataRawRow
	for rows.Next() {
		var i GetSymbolDataRawRow
		if err := rows.Scan(
			&i.Symbol,
			&i.Ts,
			&i.BidPrice,
			&i.BidSize,
			&i.AskPrice,
			&i.AskSize,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

const getSymbolOHLC15Min = `-- name: GetSymbolOHLC15Min :many
SELECT
    s.symbol AS "symbol",
    time_bucket(INTERVAL '15 min', s.ts) AS "ts",
    FIRST(s.bid_price::REAL) AS "open_bid_price",
    MAX(s.bid_price::REAL) AS "high_bid_price",
    MIN(s.bid_price::REAL) AS "low_bid_price",
    LAST(s.bid_price::REAL) AS "close_bid_price",
    FIRST(s.ask_price::REAL) AS "open_ask_price",
    MAX(s.ask_price::REAL) AS "high_ask_price",
    MIN(s.ask_price::REAL) AS "low_ask_price",
    LAST(s.ask_price::REAL) AS "close_ask_price"
FROM symbol_bid_ask AS s
GROUP BY s.symbol, s.ts
HAVING
    s.symbol = ANY($1::VARCHAR[]) AND
    s.ts >= $2 AND
    s.ts <= $3
`

type GetSymbolOHLC15MinParams struct {
	Symbol  []string           `json:"symbol"`
	TsStart pgtype.Timestamptz `json:"ts_start"`
	TsEnd   pgtype.Timestamptz `json:"ts_end"`
}

type GetSymbolOHLC15MinRow struct {
	Symbol        string      `json:"symbol"`
	Ts            interface{} `json:"ts"`
	OpenBidPrice  interface{} `json:"open_bid_price"`
	HighBidPrice  interface{} `json:"high_bid_price"`
	LowBidPrice   interface{} `json:"low_bid_price"`
	CloseBidPrice interface{} `json:"close_bid_price"`
	OpenAskPrice  interface{} `json:"open_ask_price"`
	HighAskPrice  interface{} `json:"high_ask_price"`
	LowAskPrice   interface{} `json:"low_ask_price"`
	CloseAskPrice interface{} `json:"close_ask_price"`
}

func (q *Queries) GetSymbolOHLC15Min(ctx context.Context, arg GetSymbolOHLC15MinParams) ([]GetSymbolOHLC15MinRow, error) {
	rows, err := q.db.Query(ctx, getSymbolOHLC15Min, arg.Symbol, arg.TsStart, arg.TsEnd)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []GetSymbolOHLC15MinRow
	for rows.Next() {
		var i GetSymbolOHLC15MinRow
		if err := rows.Scan(
			&i.Symbol,
			&i.Ts,
			&i.OpenBidPrice,
			&i.HighBidPrice,
			&i.LowBidPrice,
			&i.CloseBidPrice,
			&i.OpenAskPrice,
			&i.HighAskPrice,
			&i.LowAskPrice,
			&i.CloseAskPrice,
		); err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

type InsertSymbolDataParams struct {
	Symbol   string             `json:"symbol"`
	Ts       pgtype.Timestamptz `json:"ts"`
	BidPrice float64            `json:"bid_price"`
	BidSize  float64            `json:"bid_size"`
	AskPrice float64            `json:"ask_price"`
	AskSize  float64            `json:"ask_size"`
}
