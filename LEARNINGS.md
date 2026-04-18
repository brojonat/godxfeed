# Learnings

Hard-won pitfalls worth remembering. Each entry: what happened, why it was
surprising, how to avoid it next time.

## tastytrade session auth is dead (since 2024-12-01)

**What:** The `POST /sessions` endpoint that exchanged username/password for
a session token has been decommissioned. Any code doing `Authorization: <raw
session token>` will 4xx against prod.

**Why surprising:** Old blog posts / SDK examples / community code still
show the session-token flow. Even `tastytrade-api-js` kept the class around
through v6 before dropping it in v7.

**Fix:** OAuth 2.0 refresh-token grant. Mint a Personal Grant in the
tastytrade web UI (client_id + client_secret + long-lived refresh_token),
then `POST /oauth/token` with
`{grant_type: "refresh_token", refresh_token, client_secret}` to get a
~15-min access token. Use `Authorization: Bearer <access>` on every
downstream call. `service/oauth.go` implements this; `TokenProvider` caches
the access token and dedups concurrent refreshes via singleflight.

## Sandbox vs prod: different host, not a subdomain

**What:** The tastytrade sandbox is at `api.cert.tastyworks.com`, not a
subdomain of prod. Same with dxLink — the URL returned from
`/api-quote-tokens` is what you should dial, not the one in your config.

**Fix:** Two env files: `service/.env.dev` (sandbox) and `service/.env.prod`
(production). `TW_API_HOST` and `TW_OAUTH_TOKEN_URL` envs pick the flavor.
The ingress goroutine uses the `dxlink-url` field from
`/api-quote-tokens` and only falls back to the configured `--dxfeed-endpoint`
when the response is empty.

## dxFeed sends `"NaN"` as a JSON string

**What:** Where you'd expect a floating-point `NaN` (which is non-standard
in JSON), dxFeed emits the four characters `"NaN"`. `json.Unmarshal` on a
float field refuses it.

**Fix:** Byte-level `bytes.ReplaceAll(raw, []byte(\`"NaN"\`), []byte(\`0.0\`))`
on the raw message before any JSON parsing. Lives in
`service/ingress.go:parseFeedData`. Covered by `TestFeedEvents_SanitizesNaN`.

## Mermaid node syntax `[/text/]` is a parallelogram

**What:** Writing `OAUTH[/oauth/token]` in a Mermaid flowchart gives a
lexer error — `[/.../]` is the parallelogram (input/output) shape, and
the first `/` trips the lexer into that mode.

**Fix:** Quote any label that contains a leading `/`:
`OAUTH["POST /oauth/token"]`. There's a tiny
`artifacts/mermaid-preview.html` scaffold that can be regenerated from
the README block and opened in a browser to validate parse before
committing.

## NATS ACL `godxfeed.*` vs `godxfeed.>`

**What:** The NATS auth callout originally handed browsers permission to
`Sub: godxfeed.*`. The `*` wildcard matches **exactly one token** after the
prefix, so `godxfeed.SPY` works but `godxfeed.quote.SPY` would be rejected
silently.

**Fix:** Use `godxfeed.>` (any number of tokens) in
`service/nats_auth_service.go` so future subject schemes work without
re-minting user JWTs. Noticed while planning Phase 3's per-event-type
subjects.

## tmux session-name collision is a footgun

**What:** `make dev-up` creating a session named `godxfeed`, then
`make dev-down` killing `godxfeed`, kills **any** tmux session with that
name — including the user's own session they happened to name after the
project. Happened twice before the fix.

**Fix:** Namespace the dev-services session unambiguously as
`godxfeed-dev` (`TMUX_SESSION ?= godxfeed-dev`, overridable). Also make
`dev-down` refuse to proceed if `tmux display-message -p
'#{session_name}'` equals the target session — it checks whether the
invoking shell is attached to the thing it's about to kill.

## `pkill -f` is a blunt instrument

**What:** Ran `pkill -f "./cli run http-server"` to "tear down" a
background service. tmux closes panes whose foreground command exits, so
this killed not just my process but also the user's tmux window that had
been running that process.

**Fix:** Never pkill on processes you didn't start, and even for ones you
did, prefer targeted stops (sending SIGTERM to a specific PID you tracked,
or asking tmux to kill just the target pane). When in doubt, ask before
running.

## The Anthropic OAuth token-endpoint wire format is JSON, not form-encoded

**What:** Defaulted to RFC-6749 `application/x-www-form-urlencoded` when
implementing the `TokenProvider`. tastytrade's endpoint specifically wants
`application/json` with the same three fields
(`{grant_type, refresh_token, client_secret}`).

**Fix:** `Content-Type: application/json` on the POST, JSON body, no
`Authorization` header on the token-endpoint request itself (client_secret
goes in the body, not Basic auth). The test
`TestTokenProvider_RequestShape` enforces this contract.

## Go 1.25 promoted `fmt.Errorf(nonConstantString)` to a hard error

**What:** After `go get golang.org/x/sync/singleflight` bumped the toolchain
to 1.25, the whole repo failed to build — `http/middleware.go` line 172
had `fmt.Errorf(v)` where `v` was a `string` (not a format literal).
Previously a `go vet` warning; now a build failure.

**Fix:** Always format as `fmt.Errorf("%s", v)` or
`errors.New(v)`. If a toolchain bump suddenly fails, check `go vet` output
first — the compiler is surfacing old latent warnings.

## `httptest.NewServer` is the cheapest way to test wire-format code

**What:** Writing unit tests for the TokenProvider and streamer-token fetch
could have involved an interface-heavy mock setup. Instead,
`httptest.NewServer` with a plain `http.HandlerFunc` gave us complete
control over the response bytes while letting us inspect exactly what the
client sent.

**Fix:** For any external-HTTP client code, reach for `httptest.NewServer`
before considering interface extraction. Inject the `*http.Client` and a
clock function so tests can exercise retry / cache-expiry paths with
zero real time elapsed.

## Sink registry tests that reset global state need save-and-restore

**What:** First cut of `analytics.sink_test.go` called `resetRegistryForTest()`
before each test to isolate registrations. But the timescale sink registers
itself in an `init()` that only runs once — so after the first reset, the
"postgres" scheme was gone and the `TestTimescaleSinkIsRegistered` sanity
check failed.

**Fix:** `snapshotRegistryForTest` / `restoreRegistryForTest` pair. Save
the init-time state, mutate freely during the test, restore on cleanup.
Pattern generalizes to any package-level registry.
