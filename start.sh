#!/usr/bin/env bash
set -euo pipefail

AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT:-3001}
AUTH_WAIT_TIMEOUT=${AUTH_WAIT_TIMEOUT:-30}
MAX_AUTH_ATTEMPTS=${AUTH_WAIT_TIMEOUT}
PORT=${PORT:-8080}

log() {
  echo "[$(date +"%Y-%m-%dT%H:%M:%S%z")] $*"
}

cleanup_node() {
  if [[ -n "${AUTH_PID:-}" ]]; then
    if ps -p "${AUTH_PID}" >/dev/null 2>&1; then
      log "Stopping auth browser service (pid ${AUTH_PID})"
      kill "${AUTH_PID}" >/dev/null 2>&1 || true
    fi
  fi
}

# CHANGE 1: do not run cleanup on script exit (prevents self shutdown)
trap cleanup_node SIGINT SIGTERM

export PORT

log "start.sh invoked (cwd: $(pwd))"
log "Configured ports: GO_SERVER=${PORT}, AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT}"

log "Starting Go server first so Render detects the public port..."
env PORT="${PORT}" ./server &

GO_PID=$!

# CHANGE 2: replace sleeps with active port wait
log "Waiting for Go server to open port..."
until curl -s http://127.0.0.1:${PORT} >/dev/null 2>&1; do
  sleep 1
done

log "Starting auth browser service in background..."
cd auth-browser
AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT} node login.js &
AUTH_PID=$!
cd ..

log "Auth browser service launched with PID ${AUTH_PID}"
log "Waiting for auth browser to accept connections on http://127.0.0.1:${AUTH_SERVICE_PORT}..."

attempt=1
while (( attempt <= MAX_AUTH_ATTEMPTS )); do
  response_code=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${AUTH_SERVICE_PORT}" || echo "000")
  if [[ "${response_code}" != "000" ]]; then
    log "Auth browser health probe succeeded (attempt ${attempt}/${MAX_AUTH_ATTEMPTS}, HTTP ${response_code})"
    break
  fi
  log "Auth browser health probe failed (attempt ${attempt}/${MAX_AUTH_ATTEMPTS}); retrying..."
  attempt=$((attempt + 1))
  sleep 1
done

if (( attempt > MAX_AUTH_ATTEMPTS )); then
  log "Auth browser failed to respond after ${AUTH_WAIT_TIMEOUT}s (last attempt ${MAX_AUTH_ATTEMPTS})"
  exit 1
fi

log "Auth browser listening on port ${AUTH_SERVICE_PORT} (pid ${AUTH_PID}); status HTTP ${response_code}"

wait ${GO_PID}