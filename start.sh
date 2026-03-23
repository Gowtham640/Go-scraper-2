#!/usr/bin/env bash
set -euo pipefail
export $(grep -v '^#' .env | xargs)
echo "PASSWORD_KEY=$PASSWORD_KEY"
AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT:-3001}
AUTH_WAIT_TIMEOUT=${AUTH_WAIT_TIMEOUT:-30}
PORT=${PORT:-8080}

log() {
  echo "[$(date +"%Y-%m-%dT%H:%M:%S%z")] $*"
}

########################################
# CLEANUP HANDLER
########################################

cleanup() {
  log "Shutdown signal received"

  if [[ -n "${AUTH_PID:-}" ]] && ps -p "${AUTH_PID}" >/dev/null 2>&1; then
    log "Stopping auth browser (pid ${AUTH_PID})"
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

########################################
# PORT CLEANUP (ADDITIONAL - NON-DESTRUCTIVE)
########################################

cleanup_port() {
  local PORT_TO_CLEAN=$1

  log "🧹 Checking port ${PORT_TO_CLEAN}..."

  # Get PID using port (Windows Git Bash compatible)
  PID=$(netstat -ano 2>/dev/null | grep ":${PORT_TO_CLEAN}" | grep LISTENING | awk '{print $5}' | head -n 1 || true)

  if [[ -n "${PID}" ]]; then
    log "⚠️ Port ${PORT_TO_CLEAN} is in use by PID ${PID}, killing..."

    # Use winpty for Git Bash compatibility
    winpty taskkill //PID "${PID}" //F >/dev/null 2>&1 || true

    sleep 1

    log "✅ Port ${PORT_TO_CLEAN} cleaned"
  else
    log "✅ Port ${PORT_TO_CLEAN} is free"
  fi
}

########################################
# RUN PORT CLEANUP BEFORE START
########################################

cleanup_port "${PORT}"
cleanup_port "${AUTH_SERVICE_PORT}"
########################################
# START GO SERVER
########################################

start_go() {
  log "Starting Go server..."
  env PORT="${PORT}" ./server &
  GO_PID=$!
  log "Go server PID ${GO_PID}"
}

########################################
# WAIT FOR GO SERVER
########################################

wait_for_go() {
  log "Waiting for Go server on port ${PORT}"

  while true; do

    if ! ps -p "${GO_PID}" >/dev/null 2>&1; then
      log "Go server crashed during startup"
      exit 1
    fi

    if curl -s "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; then
      log "Go server ready"
      break
    fi

    sleep 1
  done
}

########################################
# START AUTH SERVICE
########################################

start_auth() {
  log "Starting auth browser service..."

  (
    cd auth-browser
    AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT} node login.js
  ) &

  AUTH_PID=$!
  log "Auth browser PID ${AUTH_PID}"
}

########################################
# WAIT FOR AUTH SERVICE
########################################

wait_for_auth() {
  log "Waiting for auth browser on port ${AUTH_SERVICE_PORT}"

  attempts=0

  while true; do

    if ! ps -p "${AUTH_PID}" >/dev/null 2>&1; then
      log "Auth browser crashed during startup"
      exit 1
    fi

    if curl -s "http://127.0.0.1:${AUTH_SERVICE_PORT}" >/dev/null 2>&1; then
      log "Auth browser ready"
      break
    fi

    attempts=$((attempts + 1))

    if [[ "${attempts}" -ge "${AUTH_WAIT_TIMEOUT}" ]]; then
      log "Auth browser startup timeout"
      exit 1
    fi

    sleep 1
  done
}

########################################
# STARTUP SEQUENCE
########################################

start_go
wait_for_go

start_auth
wait_for_auth

########################################
# WATCHDOG LOOP
########################################

log "Entering process watchdog"

while true; do
  sleep 5

  if ! ps -p "${GO_PID}" >/dev/null 2>&1; then
    log "Go server stopped unexpectedly — restarting"
    start_go
    wait_for_go
  fi

  if ! ps -p "${AUTH_PID}" >/dev/null 2>&1; then
    log "Auth browser stopped unexpectedly — restarting"
    start_auth
    wait_for_auth
  fi
done