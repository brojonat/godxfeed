BEGIN;
CREATE EXTENSION IF NOT EXISTS timescaledb;

CREATE TABLE IF NOT EXISTS metadata (
    id VARCHAR(255) NOT NULL,
    PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS symbol_bid_ask (
    symbol VARCHAR(255) NOT NULL,
    ts TIMESTAMPTZ NOT NULL,
    bid REAL,
    bid_size INTEGER,
    ask REAL,
    ask_size INTEGER
);

COMMIT;