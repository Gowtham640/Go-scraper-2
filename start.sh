#!/usr/bin/env bash
set -euo pipefail

AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT:-3001}
AUTH_WAIT_TIMEOUT=${AUTH_WAIT_TIMEOUT:-30}
MAX_AUTH_ATTEMPTS=${AUTH_WAIT_TIMEOUT}
PORT=${PORT:-8080}

log() {
  echo "[$(date +"%Y-%m-%dT%H:%M:%S%z")] $*"
}

cleanup() {
  log "Shutdown signal received. Stopping services..."

  if [[ -n "${AUTH_PID:-}" ]] && ps -p "${AUTH_PID}" >/dev/null 2>&1; then
    log "Stopping auth browser service (pid ${AUTH_PID})"
    kill "${AUTH_PID}" || true
  fi

  if [[ -n "${GO_PID:-}" ]] && ps -p "${GO_PID}" >/dev/null 2>&1; then
    log "Stopping Go server (pid ${GO_PID})"
    kill "${GO_PID}" || true
  fi

  exit 0
}

trap cleanup SIGINT SIGTERM

export PORT

log "start.sh invoked (cwd: $(pwd))"
log "Configured ports: GO_SERVER=${PORT}, AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT}"

############################################
# START GO SERVER
############################################

start_go() {
  log "Starting Go server..."
  env PORT="${PORT}" ./server &
  GO_PID=$!
  log "Go server started with PID ${GO_PID}"
}

start_go

log "Waiting for Go server to open port ${PORT}..."

until nc -z 127.0.0.1 ${PORT}; do
  sleep 1
done

log "Go server port is open"

############################################
# START AUTH BROWSER
############################################

start_auth() {
  log "Starting auth browser service..."
  cd auth-browser
  AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT} node login.js &
  AUTH_PID=$!
  cd ..
  log "Auth browser service started with PID ${AUTH_PID}"
}

start_auth

log "Waiting for auth browser health check..."

attempt=1
while (( attempt <= MAX_AUTH_ATTEMPTS )); do
  response_code=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:${AUTH_SERVICE_PORT}" || echo "000")

  if [[ "${response_code}" != "000" ]]; then
    log "Auth browser ready (attempt ${attempt}/${MAX_AUTH_ATTEMPTS}, HTTP ${response_code})"
    break
  fi

  log "Auth browser health check failed (attempt ${attempt}/${MAX_AUTH_ATTEMPTS})"
  attempt=$((attempt + 1))
  sleep 1
done

if (( attempt > MAX_AUTH_ATTEMPTS )); then
  log "Auth browser failed to start"
  exit 1
fi

############################################
# PROCESS WATCHDOG (KEEPS BOTH ALIVE)
############################################

log "Entering process monitor loop"

while true; do
  sleep 5

  if ! ps -p "${GO_PID}" >/dev/null 2>&1; then
    log "Go server stopped unexpectedly — restarting"
    start_go
  fi

  if ! ps -p "${AUTH_PID}" >/dev/null 2>&1; then
    log "Auth browser stopped unexpectedly — restarting"
    start_auth
  fi
done