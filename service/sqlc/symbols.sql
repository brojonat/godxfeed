-- name: InsertSymbolData :copyfrom
INSERT INTO symbol_bid_ask (symbol, ts, bid_price, bid_size, ask_price, ask_size)
VALUES (@symbol, @ts, @bid_price, @bid_size, @ask_price, @ask_size);

-- name: GetSymbolDataRaw :many
SELECT
    s.symbol AS "symbol",
    s.ts AS "ts",
    s.bid_price::REAL AS "bid_price",
    s.bid_size::REAL AS "bid_size",
    s.ask_price::REAL AS "ask_price",
    s.ask_size::REAL AS "ask_size"
FROM symbol_bid_ask AS s
WHERE
    s.symbol = ANY(@symbols::VARCHAR[]) AND
    s.ts >= @ts_start AND
    s.ts <= @ts_end;

-- name: GetSymbolOHLC15Min :many
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
    s.symbol = ANY(@symbol::VARCHAR[]) AND
    s.ts >= @ts_start AND
    s.ts <= @ts_end;
