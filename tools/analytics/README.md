# godxfeed-analytics

Periodic posterior-predictive overlay publisher for godxfeed. Subscribes
to `godxfeed.quote.>`, buffers the last ~60s of mid prices per symbol, and
every `ANALYTICS_INTERVAL_S` fits a Bayesian GBM model in PyMC on the
trailing 30s:

    log(p_t / p_{t-1}) ~ Normal(0, σ)
    σ ~ HalfNormal(sigma_prior)

Inference via NUTS (small: 200 draws × 2 chains, tune=200). The
posterior-*predictive* density over the next mid is built as
`LogNormal(log p₀, σ)` averaged across the σ posterior samples and
trapz-normalized to `∫y·dx ≈ 1`, then published on
`godxfeed.analytics.posterior.<SYMBOL>` — the same payload shape the
frontend `admin_plots.js` / symbol-detail overlay already consumes.

A 5% log-return cap is applied before fitting as a defensive filter
against synth-replay-loop wraparounds and other non-market-move
discontinuities — real inter-tick log-returns at sub-second cadence
never approach 5% on liquid equities, so the cap is effectively a
data-artifact detector.

## Why is this a separate service?

Because NATS is the source of truth. Analytic sidecars subscribe to
quotes, fit models, publish densities — **they never talk to the Go
server**. That keeps them independently deployable, independently
scalable, and written in whatever language best fits the modeling job
(Python for PyMC, here).

## Install

```bash
cd tools/analytics
uv sync
```

## Run locally

Assumes NATS is listening on `nats://localhost:4222` (e.g. via
`make run-nats-server` or `make dev-up` from the repo root).

```bash
# From repo root:
make analytics-serve

# Or directly:
cd tools/analytics
NATS_URL=nats://localhost:4222 \
  NATS_GODXFEED_USER=godxfeed-server \
  NATS_GODXFEED_PASSWORD=godxfeed-server-password \
  uv run analytics-serve
```

The service starts publishing immediately on `godxfeed.analytics.posterior.<SYMBOL>`
for each symbol in `ANALYTICS_SYMBOLS` (default `SPY,AAPL`).

## Verify

Tail the analytics subject tree with the NATS CLI:

```bash
nats sub 'godxfeed.analytics.>' \
  --server=nats://godxfeed-server:godxfeed-server-password@localhost:4222
```

Or open `http://localhost:8080/admin` (the godxfeed server must be
running with NATS fan-out enabled) and watch for dashed gold overlays
on per-symbol panels.

## Env vars

| Var                      | Default                   | Purpose                                                |
| ------------------------ | ------------------------- | ------------------------------------------------------ |
| `NATS_URL`               | `nats://localhost:4222`   | NATS server                                            |
| `NATS_GODXFEED_USER`     | _none_                    | auth user (matches Go server's publish user)           |
| `NATS_GODXFEED_PASSWORD` | _none_                    | auth password                                          |
| `ANALYTICS_SYMBOLS`      | `SPY,AAPL`                | CSV list of symbols to publish overlays for            |
| `ANALYTICS_INTERVAL_S`   | `2.0`                     | seconds between publishes per symbol                   |
| `ANALYTICS_FIT_WINDOW_S` | `30.0`                    | trailing-seconds horizon for each fit                  |
| `ANALYTICS_BUFFER_WINDOW_S` | `60.0`                 | per-symbol buffer retention                            |
| `ANALYTICS_MIN_SAMPLES`  | `20`                      | skip fit unless this many ticks in the window          |
| `ANALYTICS_SIGMA_PRIOR`  | `0.01`                    | scale of the `HalfNormal` prior on σ                   |
| `ANALYTICS_DRAWS`        | `200`                     | NUTS draws per chain                                   |
| `ANALYTICS_TUNE`         | `200`                     | NUTS tuning steps per chain                            |
| `ANALYTICS_CHAINS`       | `2`                       | NUTS chains                                            |
| `ANALYTICS_N_GRID`       | `100`                     | predictive-density grid resolution                     |
| `ANALYTICS_MAX_ABS_R`    | `0.05`                    | drop log-returns with `|r| >` this (data artifacts)    |
| `ANALYTICS_HOST`         | `0.0.0.0`                 | FastAPI bind host                                      |
| `ANALYTICS_PORT`         | `8090`                    | FastAPI bind port (`/healthz`, `/config`)              |
| `LOG_LEVEL`              | `INFO`                    | stdlib logging level                                   |

## Docker

```bash
make analytics-docker-build
make analytics-docker-run      # --network=host so localhost:4222 resolves
```

## Tests

```bash
make analytics-test
```

## Contract

Published payload (frontend contract lives at
`http/static/js/admin_plots.js`):

```json
{
  "type": "posterior",
  "symbol": "SPY",
  "xs": [...],
  "ys": [...],
  "at": 1718817234567
}
```

`ys` is a density (∫y·dx ≈ 1). The frontend scales it by N·dx of the
observed histogram so a well-fit curve traces the tops of the bars.
