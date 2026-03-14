#!/usr/bin/env bash
set -euo pipefail

AUTH_SERVICE_PORT=${AUTH_SERVICE_PORT:-3001}
AUTH_WAIT_TIMEOUT=${AUTH_WAIT_TIMEOUT:-30}

log() {
echo "[$(date +"%Y-%m-%dT%H:%M:%S%z")] $*"
}

log "Starting auth browser service on port ${AUTH_SERVICE_PORT}..."

cd auth-browser
node server.js &
AUTH_PID=$!
cd ..

log "Auth browser process started with PID ${AUTH_PID}"

log "Waiting for auth service to be reachable..."

for ((i=1;i<=AUTH_WAIT_TIMEOUT;i++)); do
if nc -z 127.0.0.1 ${AUTH_SERVICE_PORT}; then
log "Auth browser is ready on port ${AUTH_SERVICE_PORT}"
break
fi
log "Auth service not ready yet (${i}/${AUTH_WAIT_TIMEOUT})"
sleep 1
done

if ! nc -z 127.0.0.1 ${AUTH_SERVICE_PORT}; then
log "Auth browser did not become ready in time. Continuing anyway."
fi

PORT=${PORT:-8080}
log "Starting Go server on port ${PORT}"

exec go run main.go
