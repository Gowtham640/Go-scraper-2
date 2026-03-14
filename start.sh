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
  if [[ -n "${AUTH_PID:-}" && -e /proc/${AUTH_PID} ]]; then
    log "Stopping auth browser service (pid ${AUTH_PID})"
    kill "${AUTH_PID}" >/dev/null 2>&1 || true
  fi
}

trap 'status=$?; if (( status != 0 )); then log "start.sh aborted before Go server exec (status=${status})"; fi; cleanup_node' EXIT

export PORT

log "start.sh invoked (cwd: $(pwd))"
log "Configured ports: GO_PORT=${PORT}, AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT}"

log "Starting auth browser service in background..."
cd auth-browser
AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT} node login.js &
AUTH_PID=$!
cd ..

log "Auth browser service launched with PID ${AUTH_PID}"
log "Waiting for auth browser to accept connections on http://127.0.0.1:${AUTH_SERVICE_PORT}..."

attempt=1
until curl --silent --fail --output /dev/null "http://127.0.0.1:${AUTH_SERVICE_PORT}"; do
  if (( attempt >= MAX_AUTH_ATTEMPTS )); then
    log "Auth browser failed to respond after ${AUTH_WAIT_TIMEOUT}s (last attempt ${attempt})"
    exit 1
  fi
  log "Auth browser not ready yet (attempt ${attempt}/${MAX_AUTH_ATTEMPTS}); retrying..."
  attempt=$((attempt + 1))
  sleep 1
done

log "Auth browser responded after ${attempt} attempt(s); service is ready on port ${AUTH_SERVICE_PORT}"
log "Go server will start now (PORT=${PORT}); command: PORT=${PORT} go run main.go"
exec go run main.go
