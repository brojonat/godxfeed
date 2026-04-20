# Changelog

Notable changes, newest first. Follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

### Added
- **Real PyMC posterior in `tools/analytics/`.** Sidecar now fits a
  GBM model (`log-returns ~ Normal(0, σ)`, `σ ~ HalfNormal(0.01)`)
  via PyMC NUTS on a rolling 30s window per symbol, and publishes
  the posterior-*predictive* density over the next mid price
  (LogNormal(log p₀, σ) averaged across σ samples, trapz-normalized)
  on the existing `godxfeed.analytics.posterior.<SYMBOL>` contract.
  The ingest task owns one `godxfeed.*` subscription (single-token
  wildcard → matches quote subjects, naturally skips the analytics
  subtree), and per-symbol `QuoteBuffer`s retain 60s of mids. Fits
  run under `asyncio.to_thread` so NUTS doesn't block the loop.
  First fit pays ~10s PyTensor compile; subsequent fits are 1-3s on
  N≈60 samples. Config is env-driven:
  `ANALYTICS_{FIT_WINDOW_S,BUFFER_WINDOW_S,MIN_SAMPLES,SIGMA_PRIOR,DRAWS,TUNE,CHAINS,N_GRID,MAX_ABS_R}`.
  New modules `analytics.buffer` (`QuoteBuffer`) and
  `analytics.posterior` (`fit_sigma_posterior`, `predictive_density`,
  `mids_to_log_returns`). Replaces the dummy Gaussian publisher —
  the old `SymbolState`/`drift`/`gaussian_density` API is gone.
  24 tests pass, including a synthetic-truth σ-recovery NUTS
  integration test.
- **User-facing symbol detail page
  (`/plots?plot_kind=symbol_detail&symbol=SPY`).** First non-admin
  dashboard: pure NATS client with no dxLink control surface. Two
  live panels — a rolling 60s time-series of bid/ask (D3 step lines
  over `godxfeed.<SYMBOL>`) and a distribution panel over the last
  150 mid prices with vertical rules at the current bid and ask,
  plus any analytic posterior densities from
  `godxfeed.analytics.*.<SYMBOL>` overlaid using the same density·N·dx
  scaling as `/admin`. Posterior mean (∫ x·p(x) dx) is computed
  client-side from any overlay named `posterior` and rendered under
  the mid-price card. New files: `http/static/templates/symbol_detail.tmpl`,
  `http/static/css/symbol_detail.css`, `http/static/js/symbol_detail.js`;
  new `PlotKindSymbolDetail` case in `http/handlers_plots.go` and
  matching template registration in `http/handlers_static.go`.

### Changed
- **`tools/synth/serve.py` replays the stored trajectory in an
  infinite loop** instead of one-shot iteration. Each lap re-anchors
  `wall_start_ns` so inter-tick pacing is preserved across the wrap.
  Previously a ~20-min DuckDB would drain to KEEPALIVE-only silence
  mid-session — now dev/testing has a continuous quote stream as long
  as the synth process is up.
- **Login modal UX.** The token input now carries a hint telling
  users to run `make refresh-auth-token` and paste the `AUTH_TOKEN`
  from `service/.env.dev`, so a first-time visitor isn't stuck on an
  unlabeled password prompt. Input gets `autocomplete="off"` and a
  non-generic `name` to stop password managers from autofilling
  adjacent inputs (e.g. the index page's symbol box).
- **Index page auto-consumes `?token=…` URL param.** Previously only
  `/admin` and `/plots/line_chart` did. `scripts/synth-up.sh` now
  works as a one-click entry point from any page in the app.
- **Index page adds a Symbol Detail plot card** as the primary entry
  point; other plot cards remain.
- **Symbol-detail layout polish.** Both panels now share a fixed 260px
  height and sit in a two-column `panels-row` grid (collapses to a
  single column under ~420px), with a `min-height` floor on the `<h2>`
  headers so wrapped titles don't vertically offset the charts.
- **Symbol-detail posterior overlay visibility.** The distribution
  panel's x-domain now unions observed-mids + current bid/ask + the
  overlay's xs range (previously only observed data), so a tight
  posterior whose support exceeds the recent-mid spread isn't mostly
  clipped off-screen. The overlay renders as a filled area with a
  thicker stroke instead of a dashed line, and the D3 transition on
  the posterior path is dropped so consecutive fits snap cleanly
  rather than morphing through visually noisy intermediate shapes.
  Tradeoff documented in-file against `admin_plots.js`, which
  deliberately keeps the old behavior.

### Fixed
- **Index page "Symbol Detail" link (and all plot cards) silently did
  nothing on stale tokens.** `index.js` intercepted clicks and called
  `/test-bearer-token`, throwing a plain `Error` on non-OK; the catch
  checked `error.status === 401` which is always `undefined` on a
  plain Error, so a failed pre-flight produced a silent navigate-to-#.
  Intercept deleted — each destination page handles its own auth
  flow from `localStorage`, so the pre-flight was redundant anyway.

### Removed
- **Go dummy NATS publisher (`cli debug publish-nats`).** The command
  published Quote-shaped JSON directly to `godxfeed.<SYMBOL>`,
  bypassing the dxLink ingress and `SubscriptionManager`. That made
  the `/admin` subscriptions table diverge from what was actually
  flowing on NATS, violating the invariant that "godxfeed.<SYMBOL> is
  owned by the dxLink ingress." Off-market dev now goes through the
  synth mock (`tools/synth/`), which the Go server consumes via the
  real dxLink protocol so `SubscriptionManager` stays authoritative.
  Deleted: `cmd/godxfeed/debug.go`, the `debug` command group, the
  `run-nats-dummy-publisher` Makefile target.

### Changed
- **`make dev-up` now uses the synth mock.** The tmux stack is
  `nats + synth-serve + http-mock` (was `nats + http-dev + dummy
  publisher`). Data path: synth → Go dxLink ingress →
  `SubscriptionManager` → NATS → UI — the architecturally correct
  flow with no side-channels.

### Added
- **Analytics sidecar scaffold (`tools/analytics/`).** Python/FastAPI
  service that connects to NATS and periodically publishes posterior
  densities to `godxfeed.analytics.posterior.<SYMBOL>`. Ships with a
  Dockerfile, 10 pytest cases covering the density math + publish
  loop, and Makefile targets (`analytics-install`, `analytics-test`,
  `analytics-serve`, `analytics-docker-build`, `analytics-docker-run`).
  First cut emits a dummy Gaussian per symbol — the PyMC σ fit slots
  into `publisher.publish_once` once the plumbing is live. Verified
  end-to-end against a locally-running Docker container: messages hit
  NATS with the exact shape the `/admin` overlay consumes.
- **Frontend analytic overlay routing (`admin_plots.js`).** The rxjs
  pipeline now classifies messages by shape on the shared `godxfeed.>`
  wildcard: Quote events drive the histogram buffer; analytic messages
  (`{type, symbol, xs, ys, at}`) land in a per-symbol overlay Map keyed
  by `type` and render as a dashed density curve (CSS
  `.overlay-density`) scaled to expected counts per bin (density · N ·
  dx) so a well-fit posterior traces the tops of the histogram bars.
  Per-symbol scan state widened from `prices[]` to `{prices, overlays:
  Map}`. No new subscription, no server-side change — analytic sidecars
  publish directly to `godxfeed.analytics.<type>.<symbol>`.
- **Phase 2: dynamic subscription management.**
  - `dxclient.Client` split `Subscribe([]string)` into `OpenFeed()` +
    `UpdateSubscription(add, remove []FeedSub)` so the feed channel is
    opened once and incremental (add, remove) updates reuse it.
  - `service.SubscriptionManager` now takes a `FeedController` dependency
    (a narrow `UpdateSubscription`-only interface) and exposes
    `Add(event, symbol)` / `Remove(event, symbol)` / `BulkAdd` that
    orchestrate the wire update and local state together.
  - `Service.AddSubscription` / `Service.RemoveSubscription` methods
    on the service interface; backed by `POST /dxlink/subscriptions`
    and `DELETE /dxlink/subscriptions` HTTP handlers.
  - `/admin` gains an add-subscription form and per-row Remove
    buttons.
- **Synthetic data source (`tools/synth/`).** Fully decoupled Python
  project that impersonates tasty's dxLink gateway, so the Go service
  can run off-market against known ground-truth parameters.
  - `synth-generate` builds a DuckDB file with one row per tick, with
    per-symbol mid prices drawn from a PyMC prior-predictive sample.
    Stores (μ, σ, mid_start, spread_pct) in a `params` companion table
    so validators can recover the generating truth.
  - `synth-serve` is a FastAPI WebSocket endpoint that reads the DuckDB
    via ibis and replays rows at the stored tick cadence, speaking the
    full dxLink protocol (SETUP / AUTH / CHANNEL_REQUEST / FEED_SETUP
    / FEED_SUBSCRIPTION / KEEPALIVE). Auth is a noop.
  - Paths `/realtime` (matches tasty's URL) and `/ws` (alias).
- **`--dxfeed-url` CLI flag on the Go http-server** (env
  `DXFEED_URL`). When set, the ingress dials that exact URL (scheme
  included) and skips the tastytrade streamer-token fetch entirely,
  authenticating with a dummy token. Lets the server run against the
  synth mock without real OAuth credentials.
- **Per-symbol distribution plots on `/admin`** — new
  `static/js/admin_plots.js` module: NATS → rxjs `groupBy(symbol)` →
  `scan` into a 100-deep rolling buffer → `throttleTime(200ms)` →
  D3 mini-histogram (bars + density curve + last-N vertical
  recent-tick overlays). Each new symbol gets its own panel in a
  responsive CSS grid. rxjs imported directly from esm.sh — no
  bundler.
- **`scripts/synth-up.sh` / `scripts/synth-down.sh`** — idempotent
  one-shot launcher/stopper. Up ensures `uv sync`, generates
  `quotes.duckdb` if missing, starts `synth-serve` and the Go
  http-server in mock mode (skipped when already listening), mints a
  fresh `AUTH_TOKEN`, and opens `/admin?token=…` in the default
  browser.
- **Makefile targets**: `synth-install` / `synth-generate` /
  `synth-serve` / `run-http-mock` / `tail-synth-log`.
- **Project goals** section in the README, articulating the
  three-layer design: dxclient → UI → LLM-powered natural-language
  interface.
- **7 new dxclient tests** in `feed_test.go` driving the OpenFeed /
  UpdateSubscription split against an in-test websocket harness (the
  existing `mock_server` didn't respond to CHANNEL_REQUEST or
  FEED_SETUP, so the old `Subscribe()` was effectively untestable).
- **6 new / rewritten SubscriptionManager tests** covering Add/Remove
  idempotency, error roll-back, and the client error ↛ state change
  contract.

### Changed
- `dxclient.Client.Subscribe([]string)` removed from the interface
  (the sole caller, `service.StartIngress`, was updated). Feed
  subscription is now always `OpenFeed` (once) + `UpdateSubscription`
  (per change).
- `service.StartIngress` opens the feed exactly once and constructs
  the `SubscriptionManager` after the dxclient is live, making the
  dxLink dependency explicit rather than hidden behind a state-only
  manager.
- **`/admin` and `/plots` HTML are now public.** The server-side
  auth guard blocked the pages' own localStorage + login-modal JS
  flow (browsers can't send an `Authorization` header on a plain
  navigation — users landed on a 401 before any JS ran). All data
  endpoints (`/dxlink/*`, `/stream`, `/token`, `/webhook/*`) remain
  bearer-gated, so no content is exposed.

### Fixed
- **Go-side dxclient handler-dispatch race on sequential subscribes.**
  Initial `StartIngress` did `for sym := range symbols { subs.Add(sym) }`
  — call #1's ack handler removed itself and unblocked call #1 *before*
  call #2 registered its handler. The ack for call #2 then landed in
  that window and the dispatcher snapshotted an empty handlers list,
  dropping the message. `SubscriptionManager.BulkAdd` now batches
  startup into a single wire call with a single ack, closing the
  window. Runtime add/remove from `/admin` still uses per-symbol
  `Add`/`Remove`.
- **`dxclient.(*client).OpenFeed` deadlock.** Held `c.lock` then
  called `getNextChanID` which re-acquired it (`sync.RWMutex` isn't
  reentrant). Factored out `getNextChanIDLocked`.
- **`tools/synth/serve.py`** — three bugs caught by end-to-end
  testing:
  1. Concurrent Starlette WebSocket writers (request handler +
     replay task both calling `await ws.send_text(...)`). Serialized
     through an `asyncio.Lock` on the session.
  2. Eager symbol filter in `replay` meant symbols added after task
     start never streamed. Now queries all rows and filters per-tick
     against the live subscription set.
  3. `ibis.to_pandas()` is synchronous and was blocking the event
     loop for ~1.2s, delaying `FEED_CONFIG` acks. Moved to
     `asyncio.to_thread`.

## [2026-04-17] — Phase 1: OAuth + NATS-as-SoT

### Added
- **OAuth2 Personal Grant authentication** for tastytrade. `service/oauth.go`
  owns a singleflight-protected `TokenProvider` that refreshes a 15-min access
  token from a long-lived refresh token. Covered by 8 unit tests against an
  `httptest.Server` that mimics the tastytrade wire format.
- **`service/analytics.Sink` interface** + DSN-scheme registry. Sinks
  register themselves via `init()` and are built from DSNs on
  `--analytic-sink`. First implementation: `timescale.go` (registered for
  `postgres://` and `postgresql://`).
- **`SubscriptionManager`** in `service/subscriptions.go`. Tracks per-
  `(event, symbol)` state with atomic counters + first/last-seen timestamps.
  Lock-free on the hot path.
- **`/dxlink/status` and `/dxlink/subscriptions`** HTTP endpoints.
- **`/admin` page** with connection status, subscriptions table, and live
  `godxfeed.>` tail (ring buffer, pause/clear, msg-rate counter).
- **`make dev-up` / `dev-attach` / `dev-down` / `dev-status`** — tmux-driven
  dev stack targets. Session is namespaced `godxfeed-dev` so it can't
  collide with a user's own tmux session named after the project.
  `dev-down` refuses to kill a session you're attached to.
- **Fail-fast env validation** at every CLI entry point
  (`cmd/godxfeed/validate.go`). Missing vars surface in a single error
  listing every missing flag.
- **Mermaid architecture diagram** in README.
- **Bookkeeping docs** — `TODO.md`, `CHANGELOG.md`, `LEARNINGS.md`.

### Changed
- **NATS is now the single source of truth.** The old dual-path (one
  goroutine wrote to DB, another published to NATS) collapsed to one
  ingress goroutine that publishes to NATS. TimescaleDB persistence is
  now an independent NATS subscriber (`service/analytics/timescale.go`).
- **Raw-bytes end-to-end pipeline.** The ingress now extracts each event's
  `eventSymbol` header from a `FEED_DATA` envelope and publishes the raw
  per-event JSON to NATS without re-marshaling through a struct. Old
  `FeedCompactQuote` round-trip (with its hardcoded
  `AskPriceTheo: 123, BidPriceTheo: 789` values) is gone.
- **NATS auth callout ACL** widened from `godxfeed.*` (single token) to
  `godxfeed.>` (any number of tokens) to support future subject schemes.
- **Service interface slimmed** — removed `DBPool`, `DBQ`,
  `StartSymbolStream`, `StopSymbolStream`, `IsSymbolStreamActive`,
  `StreamAPIFeedCompactQuoteData`, `PublishSymbolData`,
  `RecordSymbolData`, `StreamHistoricalSymbolData`. Added `StartIngress`,
  `Subscriptions`, `DXLinkStatus`.
- **CLI flags rewired** — `--analytic-sink` (repeatable, env
  `ANALYTIC_SINKS`) replaces `--handler-persist` / `--handler-debug` /
  `--database`.
- **Env split** — `service/.env.dev` (sandbox) separate from
  `service/.env.prod` (production). Dev targets source `.env.dev`; k8s
  deploy sources `.env.prod`. `make env-prod` seeds prod from dev.
- **CLI basic-auth envs renamed**: `TW_USERNAME`/`TW_PASSWORD` →
  `GODXFEED_ADMIN_EMAIL` + `SERVER_SECRET_KEY` (reusing the server's
  own JWT-signing secret rather than duplicating).
- **Dummy publisher** emits Quote-shaped JSON
  (`{eventType, eventSymbol, bidPrice, askPrice, bidSize, askSize}`)
  instead of a raw float.

### Removed
- Legacy tastytrade username/password session-token auth
  (`NewSessionToken`, `TestSessionToken`, old `NewStreamerToken`). Session
  auth was deprecated by tastytrade on 2024-12-01.
- `http/handlers_timeseries.go` — relied on the service's DB pool, which
  is gone. Read-side DB access is out of scope until a proper reader
  surface is designed (see TODO).
- `Service.StreamHistoricalSymbolData` — dead method, never correctly
  implemented.
- `--handler-debug` / `--handler-persist` CLI flags (replaced by sinks).
- Duplicated env vars: `TW_USERNAME`, `TW_PASSWORD`, `SESSION_TOKEN`,
  `STREAMER_TOKEN`.

### Fixed
- `http/middleware.go` — `fmt.Errorf(v)` with a non-constant format string
  broke the build under go1.25 (the x/sync dependency bump upgraded the
  toolchain; this was latent before).
- Timescale sink was logging "skip unparseable message" for every tick
  because the dummy publisher still emitted raw floats. Publisher now
  emits proper Quote JSON, sink is happy.
