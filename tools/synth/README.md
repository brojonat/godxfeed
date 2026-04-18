# godxfeed-synth

A Python tool that **impersonates tasty's dxLink WebSocket gateway** so the
Go service can be driven against synthetic data — no tastytrade OAuth, no
market hours dependency, and with known ground-truth parameters.

Two stages, deliberately decoupled:

1. **`synth-generate`** builds a DuckDB file with one row per tick.
   Time series are drawn from a PyMC prior-predictive sample, so the
   generative parameters (drift μ, volatility σ, etc.) are written to
   disk alongside the quotes and can be read back by downstream
   inference code for validation.

2. **`synth-serve`** runs a FastAPI WebSocket server that speaks the
   dxLink protocol. When a client connects, authenticates (noop — any
   token works), and subscribes, the server replays the DuckDB rows
   **at the stored tick cadence**. From the Go client's perspective
   the mock is indistinguishable from tasty's gateway at the wire.

## Install

```bash
cd tools/synth
uv sync        # or: make synth-install from the repo root
```

## End-to-end

From the repo root:

```bash
# 1. Generate a 600-tick × 2-symbol fixture.
make synth-generate

# 2. Run the mock dxLink server on :9999.
make synth-serve   # leave this running in another terminal

# 3. Run the Go server pointed at the mock.
make run-http-mock
```

Open [http://localhost:8080/admin](http://localhost:8080/admin),
paste your AUTH_TOKEN, and watch quotes flow through `godxfeed.>`.
The `/admin` page's add/remove controls work as usual —
subscribing from the UI dispatches a real `FEED_SUBSCRIPTION` to
the mock, and the mock starts replaying the matching rows.

## CLI

### `synth-generate`

```
usage: synth-generate [-h] [--symbols SYMBOLS [SYMBOLS ...]]
                      [--n-ticks N_TICKS] [--tick-ms TICK_MS]
                      [--start START] [--mid-start MID_START]
                      [--mu MU] [--sigma SIGMA] [--spread-pct SPREAD_PCT]
                      [--seed SEED] [--out OUT]
```

Key knobs:

- `--mu`, `--sigma` — per-tick log-return drift and vol. Stored in the
  `params` table of the output DuckDB so validators can recover the
  generating truth.
- `--tick-ms` — tick cadence.
- `--n-ticks` — rows per symbol.

Writes two tables to `--out`:

- `quotes(ts_ns, symbol, event_type, bid_price, ask_price, bid_size, ask_size)`
- `params(symbol, mid_start, mu, sigma, spread_pct)`

### `synth-serve`

```
usage: synth-serve [-h] --db DB [--host HOST] [--port PORT]
                   [--speed SPEED] [--log-level LEVEL]
```

- `--db` — the DuckDB file built by `synth-generate`.
- `--speed` — playback-speed multiplier. `2.0` plays twice as fast;
  `10.0` drains a 10-minute fixture in 60s for quick smoke tests.
- WS paths: `/realtime` (matches tasty's URL) and `/ws` (alias).

## Design

The mock maintains per-connection state in a `Session` dataclass — no
globals, so multiple clients can connect concurrently. Playback is a
single `asyncio.Task` per session that walks the DuckDB table in
timestamp order and sleeps until each row's wall-clock scheduling
point. The subscription set is checked on every row, so
late-subscribes or unsubscribes take effect immediately without
restarting the task.

`auth` is a noop — the real tastytrade gateway validates the streamer
token issued by `POST /api-quote-tokens`; the mock accepts any token
and always replies `AUTH_STATE_AUTHORIZED`. See
`src/synth/protocol.py` for the wire-level message-type constants,
kept in sync with `dxclient/message.go`.

## Validation loop

The point of this setup is to make the Bayesian σ sidecar (see repo
`TODO.md`) end-to-end testable:

1. `synth-generate --mu X --sigma Y` writes known true parameters.
2. `synth-serve` + `run-http-mock` streams those quotes through the
   full pipeline.
3. The sidecar subscribes to `godxfeed.>`, does inference, and its
   posterior should concentrate around `(X, Y)` read from the
   `params` table.

If that loop closes, the pipeline and the inference code agree on the
data-generating process.
