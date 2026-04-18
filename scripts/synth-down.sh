#!/usr/bin/env bash
# Stop the synth-up.sh background processes. Idempotent.
set -euo pipefail
cd "$(dirname "$0")/.."

stop_port() {
  local port=$1 name=$2
  local pids
  pids=$(lsof -ti:"$port" 2>/dev/null || true)
  if [[ -z "$pids" ]]; then
    echo "[synth-down] $name (:$port) already stopped"
    return 0
  fi
  echo "[synth-down] stopping $name on :$port (pids: $pids)"
  # SIGTERM first, SIGKILL after 3s if still around.
  kill $pids 2>/dev/null || true
  for _ in 1 2 3; do
    sleep 1
    lsof -ti:"$port" >/dev/null 2>&1 || return 0
  done
  kill -9 $pids 2>/dev/null || true
}

stop_port 8080 "http-mock"
stop_port 9999 "synth-serve"
