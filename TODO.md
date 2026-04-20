# TODO

Flat list, one line per task, status markers: `[ ]` open, `[x]` done,
`[blocked: reason]` blocked.

## Active / near-term

- [ ] **Remove BuyMeACoffee.** Delete `POST /webhook/buy-me-a-coffee`
      (`http/handlers_webhook.go`), any BMC-signature middleware, the
      SendGrid-based JWT-email path (`service/email.go` if BMC-only),
      related env vars (`BMC_*`, `SENDGRID_*`), the README section,
      and any Makefile / CI references. Token issuance stays — it's
      now purely `POST /token` basic-auth gated.
- [x] **Frontend analytic overlay routing.** `admin_plots.js` now
      routes messages by shape on the shared `godxfeed.>` wildcard:
      Quote events feed the histogram buffer, analytic messages
      (`{type, symbol, xs, ys, at}`) land in a per-symbol overlay Map
      and render as a density curve scaled to expected counts per bin
      (density · N · dx) so the posterior traces the tops of the
      histogram bars. Adding a new analytic type = one branch in the
      classifier + one render branch. The Python producer is still
      out of scope here (see the σ-sidecar item below).
- [ ] Smoke-test the dxLink ingress against a live tastytrade sandbox
      subscription (market-hours dependent) — now exercises Phase 2's
      dynamic add/remove too.
- [x] Full Go↔Python synth-mode smoke test: confirmed quotes flow
      through `/admin` + NATS (SPY 218 msgs, AAPL 7 msgs after a
      remove→re-add cycle via the DELETE/POST endpoints). See
      `artifacts/synth-integration/`.
- [x] Phase 2 admin page: dynamic subscription management —
      `POST /dxlink/subscriptions` + `DELETE /dxlink/subscriptions` plus
      form/table controls on `/admin`. (dxclient split into
      OpenFeed+UpdateSubscription; SubscriptionManager now owns the
      dxclient.)
- [ ] Phase 3 data model: multi-event-type subjects (`godxfeed.quote.SPY`,
      `godxfeed.greeks.<opt>`, `godxfeed.theoprice.<opt>`,
      `godxfeed.underlying.<sym>`) and widen the dxLink `FEED_SUBSCRIPTION` to
      match. FEED_SETUP's AcceptEventFields is still hard-coded to Quote —
      that's the wire-level knob Phase 3 needs to widen.
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
- [ ] LLM-driven voice/text → `POST /dxlink/subscriptions` translation (depends
      on Phase 2).

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
- [ ] `dxclient/client_test.go` has a pre-existing teardown goroutine leak /
      keepalive panic in the mock server. Fix or skip during `go test ./...`.
- [ ] Retry/backoff on the dxLink connection (currently one-shot at ingress
      start).
- [x] Decide whether `SubscriptionManager` owns the dxLink client (enables Phase
      2 add/remove) or stays state-only. — Owns it, via a small
      `FeedController` interface (Accept interfaces, return structs).
- [ ] Second analytic sink implementation to validate the interface —
      candidates: `file://` (JSONL append), `clickhouse://`, `parquet://`.

## Docs / tooling

- [ ] Market-hours smoke test run + refresh the
      `artifacts/validation-report.html` against real quotes.
- [ ] Document the admin page screenshots / usage in README once Phase 2 lands.
