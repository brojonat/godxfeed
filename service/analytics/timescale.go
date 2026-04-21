package analytics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/brojonat/godxfeed/service/db/dbgen"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
)

// Phase 3 subject scheme: `godxfeed.<event>.<symbol>`. The sink is
// intentionally quote-only — Greeks / TheoPrice / Underlying carry a
// different payload shape and would need their own hypertables.
// `godxfeed.quote.>` matches `godxfeed.quote.SPY`, `godxfeed.quote.AAPL`,
// and any further token-depth we add under quote (e.g. per-venue).
const natsSubject = "godxfeed.quote.>"

func init() {
	// pgx accepts both `postgres://` and `postgresql://` — register both so
	// operators aren't surprised by which one the env file happens to use.
	RegisterSink("postgres", newTimescaleSink)
	RegisterSink("postgresql", newTimescaleSink)
}

// timescaleSink subscribes to godxfeed.quote.> and batches incoming Quote
// events into the symbol_bid_ask hypertable.
type timescaleSink struct {
	dsn  string
	nc   *nats.Conn
	pool *pgxpool.Pool
	q    *dbgen.Queries
	log  *slog.Logger
}

func newTimescaleSink(ctx context.Context, dsn string, nc *nats.Conn, log *slog.Logger) (Sink, error) {
	if nc == nil {
		return nil, fmt.Errorf("timescale sink: nats connection is required")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("timescale sink: pgxpool: %w", err)
	}
	// Fail fast if the DB is unreachable rather than letting the first insert
	// discover it — matches our fail-fast philosophy elsewhere.
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("timescale sink: ping: %w", err)
	}
	return &timescaleSink{
		dsn:  dsn,
		nc:   nc,
		pool: pool,
		q:    dbgen.New(pool),
		log:  log.With("sink", "timescale"),
	}, nil
}

func (t *timescaleSink) Name() string { return "timescale" }

func (t *timescaleSink) Start(ctx context.Context) error {
	defer t.pool.Close()

	sub, err := t.nc.Subscribe(natsSubject, func(msg *nats.Msg) {
		params, err := parseQuoteInsert(msg.Data, time.Now())
		if err != nil {
			// One malformed message shouldn't kill the sink; log and move on.
			t.log.Debug("skip unparseable message", "subject", msg.Subject, "err", err)
			return
		}
		if _, err := t.q.InsertSymbolData(ctx, []dbgen.InsertSymbolDataParams{params}); err != nil {
			t.log.Error("insert failed", "symbol", params.Symbol, "err", err)
		}
	})
	if err != nil {
		return fmt.Errorf("timescale sink: subscribe: %w", err)
	}
	defer sub.Unsubscribe()

	t.log.Info("timescale sink running", "subject", natsSubject)
	<-ctx.Done()
	t.log.Info("timescale sink stopping", "err", ctx.Err())
	return nil
}

// parseQuoteInsert parses a raw NATS message payload (a single dxlink Quote
// event) into InsertSymbolDataParams. Kept separate from the runtime so it
// can be unit-tested without a DB.
func parseQuoteInsert(payload []byte, ts time.Time) (dbgen.InsertSymbolDataParams, error) {
	var q struct {
		EventType   string  `json:"eventType"`
		EventSymbol string  `json:"eventSymbol"`
		BidPrice    float64 `json:"bidPrice"`
		AskPrice    float64 `json:"askPrice"`
		BidSize     float64 `json:"bidSize"`
		AskSize     float64 `json:"askSize"`
	}
	if err := json.Unmarshal(payload, &q); err != nil {
		return dbgen.InsertSymbolDataParams{}, fmt.Errorf("parse quote: %w", err)
	}
	if q.EventType != "" && q.EventType != "Quote" {
		return dbgen.InsertSymbolDataParams{}, fmt.Errorf("not a Quote event (got %q)", q.EventType)
	}
	if q.EventSymbol == "" {
		return dbgen.InsertSymbolDataParams{}, fmt.Errorf("missing eventSymbol")
	}
	return dbgen.InsertSymbolDataParams{
		Symbol:   q.EventSymbol,
		Ts:       pgtype.Timestamptz{Time: ts, Valid: true},
		BidPrice: q.BidPrice,
		BidSize:  q.BidSize,
		AskPrice: q.AskPrice,
		AskSize:  q.AskSize,
	}, nil
}
