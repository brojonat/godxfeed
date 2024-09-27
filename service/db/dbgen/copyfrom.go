// Code generated by sqlc. DO NOT EDIT.
// versions:
//   sqlc v1.26.0
// source: copyfrom.go

package dbgen

import (
	"context"
)

// iteratorForInsertSymbolData implements pgx.CopyFromSource.
type iteratorForInsertSymbolData struct {
	rows                 []InsertSymbolDataParams
	skippedFirstNextCall bool
}

func (r *iteratorForInsertSymbolData) Next() bool {
	if len(r.rows) == 0 {
		return false
	}
	if !r.skippedFirstNextCall {
		r.skippedFirstNextCall = true
		return true
	}
	r.rows = r.rows[1:]
	return len(r.rows) > 0
}

func (r iteratorForInsertSymbolData) Values() ([]interface{}, error) {
	return []interface{}{
		r.rows[0].Symbol,
		r.rows[0].Ts,
		r.rows[0].BidPrice,
		r.rows[0].BidSize,
		r.rows[0].AskPrice,
		r.rows[0].AskSize,
	}, nil
}

func (r iteratorForInsertSymbolData) Err() error {
	return nil
}

func (q *Queries) InsertSymbolData(ctx context.Context, arg []InsertSymbolDataParams) (int64, error) {
	return q.db.CopyFrom(ctx, []string{"symbol_bid_ask"}, []string{"symbol", "ts", "bid_price", "bid_size", "ask_price", "ask_size"}, &iteratorForInsertSymbolData{rows: arg})
}
