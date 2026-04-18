# TODO

Flat list, one line per task, status markers: `[ ]` open, `[x]` done,
`[blocked: reason]` blocked.

## Active / near-term

- [ ] Smoke-test the dxLink ingress against a live tastytrade sandbox subscription (market-hours dependent).
- [ ] Phase 2 admin page: dynamic subscription management — `POST /dxlink/subscriptions` + `DELETE /dxlink/subscriptions` plus a form/table controls on `/admin`.
- [ ] Phase 3 data model: multi-event-type subjects (`godxfeed.quote.SPY`, `godxfeed.greeks.<opt>`, `godxfeed.theoprice.<opt>`, `godxfeed.underlying.<sym>`) and widen the dxLink `FEED_SUBSCRIPTION` to match.
- [ ] Bayesian σ sidecar (Python/PyMC) that subscribes to `godxfeed.underlying.>` + `godxfeed.quote.>` and publishes posterior draws to `godxfeed.sigma.<symbol>`.
- [ ] LLM-driven voice/text → `POST /dxlink/subscriptions` translation (depends on Phase 2).

## UI polish

- [ ] Add rxjs (or similar) to the admin live-tail so operators can compose filter/throttle/group views. Intentionally deferred until we need operators — plain DOM append handles the current scale.
- [ ] Fix the cosmetic bug where driving the login modal with `querySelector('input')` populates the dashboard symbol field with the JWT.
- [ ] Options Grid plot (`/plots?plot_kind=options_grid`) is flagged WIP in README; either finish or delete.
- [ ] Line Chart plot lost its historical read path when `handlers_timeseries.go` was removed during the NATS-as-SoT refactor. Either add a timescale query sink or drop the endpoint.

## Infra / dev loop

- [ ] `dxclient/client_test.go` has a pre-existing teardown goroutine leak / keepalive panic in the mock server. Fix or skip during `go test ./...`.
- [ ] Retry/backoff on the dxLink connection (currently one-shot at ingress start).
- [ ] Decide whether `SubscriptionManager` owns the dxLink client (enables Phase 2 add/remove) or stays state-only.
- [ ] Second analytic sink implementation to validate the interface — candidates: `file://` (JSONL append), `clickhouse://`, `parquet://`.

## Docs / tooling

- [ ] Market-hours smoke test run + refresh the `artifacts/validation-report.html` against real quotes.
- [ ] Document the admin page screenshots / usage in README once Phase 2 lands.
