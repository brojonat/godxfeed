BEGIN;
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS metadata (
    symbol VARCHAR(255) NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::JSONB,
    PRIMARY KEY (symbol)
);

CREATE TABLE IF NOT EXISTS symbol_bid_ask (
    symbol VARCHAR(255) NOT NULL,
    ts TIMESTAMPTZ NOT NULL,
    bid_price REAL,
    bid_size INTEGER,
    ask_price REAL,
    ask_size INTEGER
);
SELECT create_hypertable('symbol_bid_ask', 'ts', if_not_exists => TRUE);
CREATE INDEX symbol_bid_ask_symbol ON symbol_bid_ask (symbol, ts);

COMMIT;