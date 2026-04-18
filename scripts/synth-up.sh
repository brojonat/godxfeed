#!/usr/bin/env bash
# Idempotent one-shot launcher for the synth (mock-mode) dev stack.
#
# What it does, in order:
#   1. uv sync'd the tools/synth venv (no-op if up-to-date).
#   2. Generate tools/synth/quotes.duckdb if missing.
#   3. Start synth-serve on :9999 (skipped if already listening).
#   4. Build ./cli and start the Go http-server in mock mode on :8080
#      (skipped if already listening).
#   5. Mint a fresh AUTH_TOKEN into service/.env.dev.
#   6. Open /admin in the default browser with the token in the URL.
#
# Re-run at will — nothing is duplicated.
#
# To force-regen the fixture:   rm tools/synth/quotes.duckdb && scripts/synth-up.sh
# To stop everything:            scripts/synth-down.sh
set -euo pipefail

cd "$(dirname "$0")/.."

log() { printf '[synth-up] %s\n' "$*" >&2; }

# --- Prereqs ----------------------------------------------------------------
command -v uv  >/dev/null || { echo "uv not installed: https://docs.astral.sh/uv/"   >&2; exit 1; }
command -v go  >/dev/null || { echo "go not installed"                                >&2; exit 1; }
[[ -f service/.env.dev ]] || { echo "service/.env.dev missing"                        >&2; exit 1; }

# --- Python venv + fixture --------------------------------------------------
log "uv sync (may no-op)"
( cd tools/synth && uv sync --quiet )

SYNTH_DB="tools/synth/quotes.duckdb"
if [[ ! -f "$SYNTH_DB" ]]; then
  log "generating fixture → $SYNTH_DB"
  make -s synth-generate >/dev/null
else
  log "reusing $SYNTH_DB ($(wc -c < "$SYNTH_DB" | tr -d ' ') bytes)"
fi

mkdir -p logs

# --- Background-start helper ------------------------------------------------
# Detaches via subshell + nohup so the child reparents to init; disown isn't
# needed and this works the same under bash and zsh.
start_if_free() {
  local port=$1 name=$2 logfile=$3 cmd=$4
  if lsof -ti:"$port" >/dev/null 2>&1; then
    log "$name already listening on :$port (skip)"
    return 0
  fi
  log "starting $name → $logfile"
  ( nohup bash -c "$cmd" >"$logfile" 2>&1 & )
}

wait_for_port() {
  local port=$1 name=$2 deadline=$(( $(date +%s) + 30 ))
  until lsof -ti:"$port" >/dev/null 2>&1; do
    (( $(date +%s) >= deadline )) && { echo "$name failed to bind :$port within 30s — see logs/" >&2; exit 1; }
    sleep 0.5
  done
}

# --- synth-serve (Python, :9999) -------------------------------------------
start_if_free 9999 "synth-serve" logs/synth.log \
  "cd tools/synth && exec uv run synth-serve --db $PWD/$SYNTH_DB --port 9999"
wait_for_port 9999 "synth-serve"

# --- http-server in mock mode (Go, :8080) ----------------------------------
log "go build -o cli ./cmd/godxfeed"
go build -o cli ./cmd/godxfeed

start_if_free 8080 "http-mock" logs/http.log \
  "set -a; . service/.env.dev; set +a; exec ./cli run http-server --dev-mode --log-level -4 --dxfeed-url ws://127.0.0.1:9999/realtime --symbols SPY --symbols AAPL"
wait_for_port 8080 "http-mock"

# --- Mint a fresh AUTH_TOKEN -----------------------------------------------
# get-bearer-token needs SERVER_SECRET_KEY from .env.dev to sign the JWT,
# so source it into this shell before invoking the CLI. GODXFEED_ENDPOINT
# and GODXFEED_ADMIN_EMAIL can be overridden via the caller's env.
log "minting AUTH_TOKEN"
set -a
. service/.env.dev
set +a
GODXFEED_ADMIN_EMAIL="${GODXFEED_ADMIN_EMAIL:-brojonat@gmail.com}" \
  GODXFEED_ENDPOINT="${GODXFEED_ENDPOINT:-http://localhost:8080}" \
  ./cli admin get-bearer-token --env-file service/.env.dev >/dev/null

TOKEN=$(grep '^AUTH_TOKEN=' service/.env.dev | cut -d= -f2-)
URL="http://localhost:8080/admin?token=${TOKEN}"

# --- Open in browser -------------------------------------------------------
log "opening browser"
if [[ "${OSTYPE:-}" == darwin* ]]; then
  open "$URL"
elif command -v xdg-open >/dev/null 2>&1; then
  xdg-open "$URL"
else
  echo "open manually: $URL"
fi

cat <<EOF

Running:
  synth-serve : ws://127.0.0.1:9999/realtime   tail -f logs/synth.log
  http-mock   : http://127.0.0.1:8080          tail -f logs/http.log

Stop: scripts/synth-down.sh
EOF
