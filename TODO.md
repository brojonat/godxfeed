# TODO

Flat list, one line per task, status markers: `[ ]` open, `[x]` done,
`[blocked: reason]` blocked.

## Active / near-term

- [x] **[CRITICAL] Fix `UpdateSubscription` FEED_CONFIG wait against real
      tastytrade.** Done 2026-04-20: `UpdateSubscription` is now
      fire-and-forget per the dxLink spec (FEED_CONFIG is only emitted
      in response to FEED_SETUP); `OpenFeed` still awaits it once
      post-FEED_SETUP, which is correct. Mocks in
      `dxclient/feed_test.go` and `tools/synth/serve.py` updated to
      match real tastytrade (no reply to FEED_SUBSCRIPTION). Added
      `TestUpdateSubscription_ReturnsWithoutAck` as regression guard.
      Verified end-to-end on live SPY/QQQ/AAPL: POST/DELETE now return
      immediately and `/dxlink/subscriptions` stays in lockstep with
      wire state.
- [x] **[CRITICAL] `SubscriptionManager` bookkeeping desync.** Resolved
      as a side effect of the fix above — with `UpdateSubscription` no
      longer timing out, there's no spurious rollback and state stays
      in lockstep with the wire. Remaining error paths (`Send` failures)
      are genuine "frame didn't leave" cases where rollback is correct.
- [x] **Start dxLink ingress unconditionally.** Done 2026-04-20:
      dropped the `len(syms) > 0` guard in
      `cmd/godxfeed/services.go` so the feed channel opens at boot
      regardless, and runtime `POST /dxlink/subscriptions` works
      against an empty-boot server. `BulkAdd` already short-circuits
      on an empty pair list, so no service-side change was needed.
      Closes `artifacts/validation-report.html` Finding 3.
- [x] Timescale sink log noise. Done 2026-04-20: narrowed the
      sink's subscription subject from `godxfeed.>` to `godxfeed.*`
      so it no longer receives (and tries to parse) analytics
      messages published on `godxfeed.analytics.<type>.<symbol>`.
      Closes `artifacts/validation-report.html` Finding 4. Phase 3
      moved the filter to `godxfeed.quote.>` (the sink is still
      Quote-only — Greeks/TheoPrice/Underlying have different
      payload shapes).
- [x] **Remove BuyMeACoffee.** Done 2026-04-20: deleted
      `http/handlers_webhook.go`, `service/email.go`, the
      `bmcWebhookAuthorizer` + `getWebhookSecret` in
      `http/middleware.go`, the `POST /webhook/buy-me-a-coffee`
      route in `http/http.go`, the Payment/Subscriptions README
      section, the `BMC_WEBHOOK_SECRET` / `SENDGRID_*` entries in
      `.env.dev`, and the `sendgrid-go` dependency via
      `go mod tidy`. Token issuance stays — `POST /token`
      basic-auth remains the sole minting path.
- [x] **Frontend analytic overlay routing.** `admin_plots.js` now
      routes messages by shape on the shared `godxfeed.>` wildcard:
      Quote events feed the histogram buffer, analytic messages
      (`{type, symbol, xs, ys, at}`) land in a per-symbol overlay Map
      and render as a density curve scaled to expected counts per bin
      (density · N · dx) so the posterior traces the tops of the
      histogram bars. Adding a new analytic type = one branch in the
      classifier + one render branch. The Python producer is still
      out of scope here (see the σ-sidecar item below).
- [x] Smoke-test the dxLink ingress against a live tastytrade sandbox
      subscription (market-hours dependent) — done 2026-04-20, see
      `artifacts/validation-report.html`. Surfaced 3 bugs (tracked
      below); OAuth, handshake, boot-time subs, NATS fanout, and the
      PyMC sidecar all worked cleanly end-to-end.
- [x] Full Go↔Python synth-mode smoke test: confirmed quotes flow
      through `/admin` + NATS (SPY 218 msgs, AAPL 7 msgs after a
      remove→re-add cycle via the DELETE/POST endpoints). See
      `artifacts/synth-integration/`.
- [x] Phase 2 admin page: dynamic subscription management —
      `POST /dxlink/subscriptions` + `DELETE /dxlink/subscriptions` plus
      form/table controls on `/admin`. (dxclient split into
      OpenFeed+UpdateSubscription; SubscriptionManager now owns the
      dxclient.)
- [x] Phase 3 data model: multi-event-type subjects
      (`godxfeed.quote.SPY`, `godxfeed.greeks.<opt>`,
      `godxfeed.theoprice.<opt>`, `godxfeed.underlying.<sym>`) and
      widen the dxLink `FEED_SETUP.AcceptEventFields` to match. Done
      2026-04-20: `service.subjectFor` now takes `(event, symbol)`;
      the timescale sink and analytics sidecar moved to
      `godxfeed.quote.>`; `/stream` accepts an `event` param
      (defaults to `quote`); frontend JS consumes the server-returned
      subject directly. Verified end-to-end against synth — runtime
      `POST /dxlink/subscriptions` with `(Greeks, TSLA)` now
      dispatches `godxfeed.greeks.TSLA` through the wire. See
      CHANGELOG entry for full surface.
- [x] Analytics sidecar scaffold (`tools/analytics/`). Python/FastAPI
      service that periodically publishes posterior densities to
      `godxfeed.analytics.posterior.<SYMBOL>`. First cut emits a dummy
      Gaussian per symbol to prove the overlay contract end-to-end —
      verified via `nats sub` against a locally-running container.
      Modeling plumbing (PyMC fit against recent quote data) is the
      next ticket.
- [x] Wire a real PyMC σ model into `tools/analytics/publisher.py`.
      Subscribes to `godxfeed.*` (quote-only wildcard; ignores the
      analytics subtree naturally), buffers 60s of mids per symbol,
      and every `ANALYTICS_INTERVAL_S` fits a NUTS model on the last
      30s: `log-returns ~ Normal(0, σ)` with `σ ~ HalfNormal(0.01)`.
      Publishes the posterior-*predictive* over next mid
      (`LogNormal(log p₀, σ)` averaged across σ samples, trapz-
      normalized) on the existing `{type:"posterior", xs, ys, at}`
      contract. 5% log-return outlier cap handles synth-lap wraparound
      artifacts. First fit pays ~10s PyTensor compile; subsequent
      fits 1-3s on N≈60 ticks.
- [x] LLM-driven text → subscription translation. Done 2026-04-20:
      `POST /nl-subscribe` accepts `{text, provider?}`, extracts a
      structured `FilterSpec` via a provider-agnostic `llm.Provider`
      interface (Anthropic / OpenAI / Gemini implementations, each
      configured by env/flag), then deterministically resolves the
      filter to concrete `(event, symbol)` pairs against tastytrade
      symbology and dispatches via `BulkAddSubscriptions`. The LLM
      never sees the option chain — it only parses intent; the
      resolver owns all expiry/strike/kind filtering. 35 llm-package
      tests + 6 handler tests. Voice input deferred to a separate
      frontend task; text handles every current use case.
- [x] **Refactor analytics sidecar to read quotes from NATS JetStream.**
      Done 2026-04-22: `QUOTES` JetStream stream over
      `godxfeed.quote.>` with 10min memory-backed retention, created
      at Go server startup (`service/jetstream.go`). Analytics sidecar
      (`tools/analytics/publisher.py`) now subscribes via JetStream
      push consumer with `DeliverPolicy.ALL` + `ordered_consumer=True`
      for replay on restart; falls back to plain NATS core subscribe
      if JetStream is unavailable. NATS config updated: `godxfeed`
      account has `jetstream: enabled`. Three new tests cover
      JetStream, fallback, and plain-NATS ingest paths.

## Follow-ups on LLM subscription interface

- [x] `/nl-subscribe` admin UI — done 2026-04-20. Textarea + provider
      dropdown on `/admin` posts to the endpoint and renders the
      echo'd `FilterSpec` + resolved subs. The subs table refreshes
      immediately after dispatch via a `nl-subscribe:applied` event.
- [x] Voice input wrapper — done 2026-04-20. Web Speech API wired
      into the admin textarea with live interim results. Feature-
      detected with a clean unsupported-browser fallback (disabled
      button + tooltip). Errors `no-speech`/`aborted` treated as
      normal user stops.
- [ ] Prompt-eval loop — golden cases (e.g. "next Friday's SPY calls"
      against a fixed date) to catch drift between provider upgrades.
- [ ] Option streamer symbols start with `.` — once options flow
      through, handle the NATS subject collision
      (`godxfeed.greeks..SPY260320C500` has an empty middle token).
      See LEARNINGS for the two fix options.

## UI polish

- [x] **User-facing symbol detail page.** First user-facing (non-admin)
      page: `/plots?plot_kind=symbol_detail&symbol=SYMBOL`. Pure NATS
      client — subscribes to `godxfeed.<SYMBOL>` for quotes and
      `godxfeed.analytics.*.<SYMBOL>` for posterior overlays.
      Renders a rolling 60s time-series of bid/ask plus a
      distribution panel over recent mids with current-bid/ask
      vertical rules and analytic density curves. No dxLink control
      surface — it's a viewer, not an admin.
- [x] **Auth modal hint + `?token=` auto-consume on index.** Modal
      now tells users to run `make refresh-auth-token` and paste
      the AUTH_TOKEN. Index page auto-consumes a `?token=…` URL
      param (previously only /admin and /plots/line_chart did),
      matching what `scripts/synth-up.sh` generates.
- [ ] **Broader non-admin NATS-subscription UI.** Per the two-layer
      subscription model, end users still need a way to compose which
      subjects they watch across multiple symbols / analytic types /
      future sidecar outputs. The symbol detail page is a per-symbol
      slice of that; a multi-subject dashboard is the next step.
- [ ] Add rxjs (or similar) to the admin live-tail so operators can compose
      filter/throttle/group views. Intentionally deferred until we need
      operators — plain DOM append handles the current scale.
- [x] Fix the cosmetic bug where driving the login modal with
      `querySelector('input')` populates the dashboard symbol field with the
      JWT. Added `autocomplete="off"` + `name` attrs to both the symbol
      input and the modal's token input so password managers / autofill
      stop conflating them.
- [ ] Options Grid plot (`/plots?plot_kind=options_grid`) is flagged WIP in
      README; either finish or delete.
- [ ] Line Chart plot lost its historical read path when
      `handlers_timeseries.go` was removed during the NATS-as-SoT refactor.
      Either add a timescale query sink or drop the endpoint.

## Infra / dev loop

- [x] Remove the Go dummy NATS publisher. `godxfeed.<SYMBOL>` is now
      exclusively produced by the dxLink ingress / `SubscriptionManager`;
      off-market dev goes through the synth mock so the stream and the
      subscriptions table stay consistent.
- [x] `dxclient/client_test.go` pre-existing teardown goroutine leak /
      keepalive panic. Done 2026-04-22: Root causes: (1) `Authenticate`
      didn't clean up its handler on the error path, leaking a handler
      that blocked `readForever`'s dispatch on subsequent messages;
      (2) `readForever`'s inner goroutine used a shared `loop` bool
      (data race) and never returned on read errors (goroutine leak);
      (3) the mock server sent one UNAUTHORIZED for a bad token but
      the client expected two (matching real tastytrade), causing an
      infinite hang. Fix: rewrote `readForever` to use a buffered
      error channel + ctx-aware sends (no shared mutable state),
      added handler cleanup on the `Authenticate` error path, added
      token validation to `fakeServer`, and rewrote `TestClientSetup`
      to use `fakeServer` (httptest, random port) instead of
      `mock_server` (hardcoded :8080). All 9 tests pass with `-race`.
- [x] Retry/backoff on the dxLink connection. Done 2026-04-30:
      `ingressWithReconnect` detects connection death via the new
      `Client.Done()` channel + `C()` channel closure, cancels the
      old client's context, waits for goroutine drain, then retries
      via `reconnectWithBackoff` (exponential 1s→60s with jitter).
      `dialAndSubscribe` extracts the connection lifecycle into a
      reusable helper; on reconnect it calls `SubscriptionManager.Rebind`
      to replay all active subscriptions on the new wire. `Send` is now
      non-blocking on dead connections (returns `ErrClient` via `done`
      channel guard). `DXLinkStatus` gains `ReconnectAttempt` and
      `LastError` fields for observability. Per-client derived contexts
      ensure clean teardown of old goroutines. 44 tests pass with
      `-race` across `dxclient/` and `service/`.
- [x] Decide whether `SubscriptionManager` owns the dxLink client (enables Phase
      2 add/remove) or stays state-only. — Owns it, via a small
      `FeedController` interface (Accept interfaces, return structs).
- [ ] Second analytic sink implementation to validate the interface —
      candidates: `file://` (JSONL append), `clickhouse://`, `parquet://`.

## Product / business model

- [ ] **Think through the "model" for this project.** Selling hosted
      access is probably off the table — redistributing dxFeed/
      tastytrade data almost certainly violates their TOS even if we
      wrap it in our own service. The cleanest alternative: keep this
      a clone-and-run tool for day traders who already have their own
      tastytrade OAuth grant. That collapses our infra exposure to
      zero and the repo becomes the product. Monetization paths that
      don't involve reselling data: a tip jar in the README, GitHub
      Sponsors for people who find it useful, or a pitch to devtool
      companies (Warp, Tailscale, etc.) once there's enough traction
      to matter. Open question worth checking before committing: does
      hosting a "bring-your-own-grant" multi-tenant version — where
      each tenant's grant authenticates their own feed — fall inside
      or outside the TOS? That'd be a middle ground.

## Docs / tooling

- [x] Market-hours smoke test run + refresh the
      `artifacts/validation-report.html` against real quotes. Done
      2026-04-20 on SPY/QQQ/AAPL; report + raw logs under
      `artifacts/validation-report/`.
- [ ] Document the admin page screenshots / usage in README once Phase 2 lands.
