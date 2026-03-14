#!/usr/bin/env bash
set -euo pipefail

AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT:-3001}
AUTH_WAIT_TIMEOUT=${AUTH_WAIT_TIMEOUT:-30}
MAX_AUTH_ATTEMPTS=${AUTH_WAIT_TIMEOUT}

log() {
  echo "[$(date +"%Y-%m-%dT%H:%M:%S%z")] $*"
}

trap 'if [[ -n "${AUTH_PID:-}" ]]; then kill "${AUTH_PID}" >/dev/null 2>&1 || true; fi' EXIT

log "Starting auth browser service on port ${AUTH_SERVICE_PORT}..."
cd auth-browser
AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT} node login.js &
AUTH_PID=$!
cd ..

log "Waiting for auth browser to accept connections..."
attempt=1
until curl --silent --output /dev/null "http://127.0.0.1:${AUTH_SERVICE_PORT}"; do
  if (( attempt >= MAX_AUTH_ATTEMPTS )); then
    log "Auth browser failed to respond after ${AUTH_WAIT_TIMEOUT}s"
    exit 1
  fi
  log "Auth browser not ready yet (attempt ${attempt}/${MAX_AUTH_ATTEMPTS})"
  attempt=$((attempt + 1))
  sleep 1
done

log "Auth browser ready, launching Go server"

exec go run main.go
