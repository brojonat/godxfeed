# Learnings

Hard-won pitfalls worth remembering. Each entry: what happened, why it was
surprising, how to avoid it next time.

## Sequential per-item subscribes race the dxclient handler dispatcher

**What:** Initial `StartIngress` did `for sym := range symbols { subs.Add(sym) }`.
Each `Add` registers a one-shot handler, sends FEED_SUBSCRIPTION, and
waits for FEED_CONFIG. Python mock acked every sub correctly within
1-2ms. Yet the SECOND ack was silently dropped by the Go client and
`awaitFeedConfig` timed out at 30s.

**Why surprising:** All sub messages were sent; all acks were received
at the wire level (confirmed by decoding `http.log`). Nothing looked
wrong from either side in isolation. The drop was invisible in any
single layer.

**Root cause:** The dispatcher snapshots handlers at message-receive
time. Call #1's handler sends on `done`, which unblocks call #1's
outer `Add`. The symbols loop advances to call #2, which then
registers its handler. The ack for call #2 can arrive between
"call #1's handler fires" and "call #2 registers its handler" —
in that window the dispatcher takes a snapshot with no matching
handler and drops the message.

**Fix:** Batch startup into a single `UpdateSubscription(add, remove)`
call — one ack, one handler, no window. `SubscriptionManager.BulkAdd`
in `service/subscriptions.go`. Runtime add/remove from `/admin` still
uses the per-symbol `Add`/`Remove` (user-initiated, no burst
pressure). The deeper dispatcher fix is out of scope for now.

## Starlette WebSocket is not multi-writer safe

**What:** `tools/synth/serve.py` had both the request handler and the
replay task calling `await ws.send_text(...)`. Outgoing frames
interleaved and the client started missing acks intermittently.

**Why surprising:** Python is single-threaded; naïve reasoning says
two coroutines can't collide. But each `send_text` goes through
several `await` points down the ASGI stack, and two send flows can
partially interleave at those yield points.

**Fix:** An `asyncio.Lock` on the session, acquired around every
send. Any WS server that has out-of-band emitters (timers, replay
tasks, pub/sub subscribers) needs this.

## Synchronous DB calls inside an asyncio handler block the event loop

**What:** `ibis.duckdb.connect(...).table("quotes").order_by(...).to_pandas()`
ran for ~1.2s on my data. During that 1.2s the entire `asyncio`
event loop was frozen — the WebSocket handler couldn't process new
messages, the other coroutines couldn't progress, `KEEPALIVE` replies
stopped.

**Why surprising:** Both the ibis and duckdb packages have excellent
in-memory performance, so I assumed the query would be negligible.
In practice even cheap queries block for long enough to disrupt a
real-time server.

**Fix:** `await asyncio.to_thread(lambda: ...)` around any sync DB
/ CPU-heavy call in an async handler. Generalizes to sync clients
for Postgres, Redis, file I/O — anything that doesn't have a
native asyncio API.

## llvmlite / numba dropped macOS x86_64 wheels at 0.46 / 0.63

**What:** `uv sync` on `tools/synth` failed building `llvmlite==0.47.0`
from source ("cmake not found"). PyMC → pytensor → numba → llvmlite is
a transitive chain.

**Why surprising:** PyPI lists arm64 macOS wheels for llvmlite 0.47,
plus manylinux + win_amd64 — but no `macosx_*_x86_64` entries. Anyone
on an Intel Mac has to build from source unless they pin older.

**Fix:** Pin `numba<0.63` and `llvmlite<0.46` in `tools/synth/pyproject.toml`
(llvmlite 0.45 was the last Intel-Mac version). Drop the constraints on
arm64 / Linux hosts. Alternative is `brew install cmake` — slower each
time.

## `sync.RWMutex` is not reentrant — deadlock from lock + self-call

**What:** First cut of `OpenFeed` did `c.lock.Lock()` and then called
`c.getNextChanID()` which itself did `c.lock.Lock()`. The second lock
deadlocked the goroutine silently — the test just hung, no panic, no
stack trace until the test framework killed it.

**Why surprising:** In some languages / other mutex types, recursive
locking is allowed or at least fails loudly. Go's `sync.Mutex` /
`sync.RWMutex` are non-reentrant and a same-goroutine re-lock just
blocks forever. There's no built-in detector.

**Fix:** Split any helper that touches `c.lock` into a public version
(that locks) and an internal `...Locked` version that the caller uses
while already holding the lock. `getNextChanIDLocked()` in
`dxclient/client.go` is the pattern. If a hang's first symptom is a
test timing out *before any wire traffic*, check for same-goroutine
re-locks before anything else.

## Real tastytrade does not emit FEED_CONFIG in response to FEED_SUBSCRIPTION — only FEED_SETUP

**What:** `UpdateSubscription` in `dxclient/client.go` sends a FEED_SUBSCRIPTION
frame and calls `awaitFeedConfig`, which blocks for 30s waiting for a
`MessageFeedConfig` ack. Against real tastytrade
(`wss://tasty-openapi-ws.dxfeed.com/realtime`), this ack never arrives —
every runtime `/dxlink/subscriptions` POST/DELETE times out. The wire
change *did* take effect (new symbols start streaming FEED_DATA, deletes
stop their flow), but the Go bookkeeping rolls back on error, producing a
total desync between `/admin` state and what's actually on the wire.

**Why surprising:** `tools/synth/serve.py` and the in-test harness
`dxclient/feed_test.go:99` both reply with FEED_CONFIG on every
FEED_SUBSCRIPTION. Every test and every off-market dev run happened to
satisfy the client. First contact with production was also first time
this ever failed.

**Fix direction:** FEED_SUBSCRIPTION is fire-and-forget per the dxLink
protocol — the arrival of FEED_DATA for the new symbol is the only
implicit ack. Drop `awaitFeedConfig` from `UpdateSubscription`; keep it
on `OpenFeed` (which legitimately receives FEED_CONFIG after FEED_SETUP).
Update the synth mock and feed_test harness to match real protocol, or
they'll keep hiding this class of bug. See
`artifacts/validation-report.html` (2026-04-20) for the live-market
evidence.

## Mock that's too forgiving is worse than no mock

**What:** The synth mock responded to every FEED_SUBSCRIPTION with a
FEED_CONFIG (because the client demanded one), so every integration test
against synth passed. Deployed to real tastytrade, every incremental
add/remove broke — the protocol mismatch had been invisible for months.

**Why surprising:** You'd think a mock that accepts what the client sends
is "correct enough." In reality, a mock that humours a broken client
teaches the client its own bugs are features. The mock has to push back
where the real server would push back.

**Fix:** When writing a protocol mock, start from the real server's
behavior (capture a session, read the spec), not from "what the client
expects." If the client times out against the real server, the mock's
job is to reproduce that timeout until the client is fixed — then the
test passes because the client is correct, not because the mock was
lenient. Every off-market validation run now has a matching on-market
sanity pass in `artifacts/validation-report.html`.

## The existing `dxclient.Client.Subscribe` couldn't be tested against the repo's own mock

**What:** Writing Phase-2 TDD tests, I went to extend the existing
`mock_server` to respond to CHANNEL_REQUEST and FEED_SETUP — and
realized it never had. Meaning the pre-existing `Subscribe` method
would hang against the mock. The pre-existing `TestClientSetup` only
covered Dial + Authenticate.

**Why surprising:** You'd assume a mock server named after a protocol
covers the protocol, or that the client's non-trivial method (open
channel, feed setup, feed subscription, all request/response) had
test coverage. Neither held.

**Fix:** Wrote a minimal in-test websocket server in
`dxclient/feed_test.go` using `httptest.NewServer` + gorilla/websocket
directly. Full control, no dependency on `mock_server`, records every
inbound message for assertions. Pattern worth re-using for any future
protocol work — one file, ~100 lines of harness, exhaustive
observability.

## Port 8080 collision makes `TestClientSetup` look broken

**What:** Running `go test ./dxclient/` while the dev server is up
fails `TestClientSetup` with "bad handshake" — the test's own mock
server can't bind to `:8080`, so the client ends up dialing the real
dev server, which doesn't speak dxlink.

**Why surprising:** The error message (handshake failure) blames the
protocol layer, not the port. If you don't notice the real server
running on the side, you chase a ghost.

**Fix:** Before reaching for "my refactor broke the mock", check
`lsof -i :8080`. Longer term, this test should bind to `:0` and
discover its own port. Logged in TODO as "pre-existing teardown leak /
keepalive panic."


## Narrow the LLM's job to extraction, not selection

**What:** First pass at `/nl-subscribe` planned to feed the LLM the
whole option chain (hundreds of strikes/expiries) and let it pick
which to subscribe. Pivoted to a two-stage pipeline instead: LLM
extracts a `FilterSpec` (root + kind + expiry window + strike
window), and a deterministic Go resolver applies that filter against
the chain.

**Why surprising:** The "open-ended picker" approach *feels* more
model-native — you're leveraging the LLM's judgment. But it trades
three concrete wins for zero real gains: (1) prompts shrink from
~50 KB option-chain blobs to ~500 bytes of user text, so cost and
latency collapse; (2) filter logic becomes unit-testable with
no LLM in the loop (12 resolver tests cover every edge case —
unreachable in any "just ask the model" design); (3) auditability —
"why did these 8 strikes get subscribed" is a trivial filter trace
instead of a prompt-engineering mystery.

**Fix direction:** Every time you're tempted to let an LLM make
structured selections over a known dataset, ask: *could this be
`filter(dataset, llm_extracted_spec)` instead of
`llm(dataset + prompt)`?* Usually yes, and the non-LLM half is
where all the testing leverage lives.

## Gemini's `responseSchema` dialect strips several JSON Schema keywords

**What:** First live smoke test of the Gemini provider (Anthropic +
OpenAI passed green immediately) failed twice with HTTP 400. Both
were schema-dialect issues not in the Google docs I'd read:

1. `additionalProperties` is rejected at every depth. Error text:
   `"Unknown name \"additionalProperties\" at
   'generation_config.response_schema.properties[1].value'"` — the
   reference to `properties[1]` was red-herring (Gemini enumerates
   map properties as a list internally) but the fix was just to
   strip the keyword recursively.

2. Empty-string `enum` members are rejected. Error text:
   `"properties[kind].enum[3]: cannot be empty"`. The shared schema
   uses `""` on `kind` to mean "equity-only" (i.e. no options);
   Gemini treats `""` as an invalid value in a string-enum list.

**Why surprising:** "Structured output" feels commoditized at a
distance, but each provider's schema parser has opinions that only
surface when you actually POST to it. Docs describe what's
supported, not what's *rejected*.

**Fix direction:** Smoke-test against real APIs before shipping
provider code. The unit tests against `httptest.NewServer` happily
accept any JSON we send — they verify shape, not semantics. Once
the schema parser on the other end weighs in, real bugs show up.
`normalizeForGemini` now strips both constructs recursively; if a
future Gemini version adds more quirks, that same function is the
right place to land them.

## Web Speech API: supported ≠ deployed everywhere

**What:** Wiring the admin textbox's voice input, I reached for
`SpeechRecognition` (via `window.SpeechRecognition ||
window.webkitSpeechRecognition`). Chrome/Edge have it. Firefox
exposes `SpeechRecognition` but returns `service-not-allowed` on
`start()` in most configurations. Safari has it behind a flag or
not at all depending on version.

**Why surprising:** MDN's compat table makes the API look broadly
available — "supported" in most modern browsers. In practice,
outside Chromium the implementation is either missing or routes to
a server that most users can't reach.

**Fix direction:** Feature-detect *and* handle `onerror` cleanly,
but don't bother polyfilling. The UI disables the mic button and
shows a tooltip ("try Chrome/Edge") when the API is absent; runtime
errors like `no-speech` and `aborted` are treated as normal
user-driven stops rather than scary alerts. If broader browser
support ever matters, the fallback is server-side STT (Whisper, et
al.) — not a polyfill.

## Each major LLM provider has a different way to force JSON output

**What:** Building provider-agnostic `llm.Provider` implementations
for Anthropic / OpenAI / Gemini. Wanted a single JSON Schema + a
single prompt to flow through all three. In practice each provider
forces structured output via a different API:

- **Anthropic**: define a tool with `input_schema`, then use
  `tool_choice: {type: "tool", name: "..."}` to force the model to
  call it. Response is a `tool_use` block with `input` as the
  parsed JSON object.
- **OpenAI**: `response_format: {type: "json_schema", json_schema:
  {schema, strict: true}}`. Response content is a JSON *string*
  matching the schema. Refusals surface as a non-empty `refusal`
  field on the message.
- **Gemini**: `generationConfig: {responseMimeType:
  "application/json", responseSchema: <schema>}`. Response text is
  the JSON string. Gemini's schema dialect also doesn't accept
  `type: ["X", "null"]` unions — nullable fields use
  `nullable: true`.

**Why surprising:** The "force structured output" feature seemed
commoditized across providers, so sharing one wire format seemed
plausible. The error paths alone make that impossible: refusals,
content-policy blocks, non-STOP finish reasons each surface at a
different JSON path.

**Fix direction:** Keep the shared abstraction at the *semantic*
level — `Extract(text, now time.Time) (FilterSpec, error)` — and let
each provider own its own wire adapter. Shared JSON schema as a
constant, but provider-specific normalization (e.g. Gemini's
`nullable`) is a one-function translation. Every provider test uses
`httptest.NewServer` to pin the exact request/response bytes —
that's what catches regressions when an upstream tweaks its API.

## dxLink `AcceptEventFields` is strictly a COMPACT-mode knob

**What:** Phase 3 widens `dxclient.client.OpenFeed`'s `FEED_SETUP.AcceptEventFields`
from `{Quote: [...]}` to also declare Greeks, TheoPrice, and Underlying.
In FULL data mode — which is what we use — the server actually sends every
field it has for each event type regardless of this map; the listed fields
don't gate the wire payload.

**Why surprising:** The name suggests the client is opting in to specific
fields. In reality it only matters for COMPACT mode, where the server
renders each event as a positional array and needs to know the column order
up front. In FULL mode each event is a self-describing object so the map
is effectively documentation.

**Fix:** Still list the event types we care about so the semantics stay
legible (a reader can see at a glance which streams the client wants), and
so a later switch to COMPACT doesn't silently regress. A later
`FEED_SUBSCRIPTION` with `type: "Greeks"` doesn't require the server to
have seen "Greeks" in AcceptEventFields, but keeping the listing correct
makes the code match the contract we'd need under COMPACT.

## NATS subject tokens can't start with a dot — matters for option streamer symbols

**What:** Phase 3's subject scheme is `godxfeed.<event>.<symbol>`. Option
streamer symbols from dxFeed typically start with a `.` (e.g.
`.SPY260321C500`). Concatenating yields `godxfeed.greeks..SPY260321C500`
— two tokens `greeks` and `` (empty), then `SPY260321C500`. NATS rejects
subjects with empty tokens ("foo..bar"), so publishing would fail.

**Why surprising:** The same hazard existed silently under the
single-token scheme (`godxfeed..SPY260321C500`) but was never exposed
because options aren't yet wired through end-to-end — equity symbols
like `SPY` / `AAPL` don't contain dots, so every test and every live
validation run happened to produce a valid subject.

**Fix direction (deferred):** When the options grid lands, the ingress
will need to munge option symbols before subject construction — either
strip the leading dot, replace all dots with an underscore, or
percent-encode. The choice has to be reversible if any consumer wants
to round-trip from subject to symbol; stripping the leading dot is
irreversible but simple. Decide when the first option-quote path is
wired up.

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
