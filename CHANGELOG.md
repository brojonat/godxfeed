# Changelog

Notable changes, newest first. Follows [Keep a Changelog](https://keepachangelog.com/).

## [Unreleased]

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
