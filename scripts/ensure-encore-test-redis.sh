#!/usr/bin/env bash
# Ensure Redis is reachable for encore test smoke (auth sessions via IssueTestAccessToken).
#
# Encore test provisions Postgres automatically but NOT Redis. Smoke tests that call
# BootstrapOwner need a live Redis matching the local RedisURL secret.
#
# Prefers 127.0.0.1 over localhost to avoid IPv6 [::1] connection refused when Redis
# only listens on IPv4.
#
# Usage:
#   ./scripts/ensure-encore-test-redis.sh
#   ./scripts/run-api-smoke-tests.sh
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

REDIS_HOST="${REDIS_HOST:-127.0.0.1}"
REDIS_PORT="${REDIS_PORT:-6379}"
CONTAINER="wabantu-apitest-redis"

redis_ping() {
  if command -v redis-cli >/dev/null 2>&1; then
    redis-cli -h "$REDIS_HOST" -p "$REDIS_PORT" ping 2>/dev/null | grep -q PONG
    return
  fi
  # Fallback when redis-cli is not installed (e.g. minimal CI images).
  (echo >"/dev/tcp/${REDIS_HOST}/${REDIS_PORT}") >/dev/null 2>&1
}

start_test_container() {
  if docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$CONTAINER"; then
    echo "Starting existing container ${CONTAINER}..."
    docker start "$CONTAINER" >/dev/null
  else
    echo "Starting Redis test container ${CONTAINER} on ${REDIS_HOST}:${REDIS_PORT}..."
    docker run -d --name "$CONTAINER" -p "${REDIS_HOST}:${REDIS_PORT}:6379" redis:7-alpine >/dev/null
  fi
}

wait_for_redis() {
  local i
  for i in $(seq 1 30); do
    if redis_ping; then
      return 0
    fi
    sleep 0.2
  done
  return 1
}

if redis_ping; then
  echo "Redis OK at redis://${REDIS_HOST}:${REDIS_PORT}"
  exit 0
fi

if ! command -v docker >/dev/null 2>&1; then
  cat >&2 <<EOF
error: Redis is not reachable at redis://${REDIS_HOST}:${REDIS_PORT} and Docker is unavailable.

Smoke tests need Redis for auth sessions (BootstrapOwner → IssueTestAccessToken).

Start Redis manually, then re-run:
  docker run -d --name ${CONTAINER} -p ${REDIS_HOST}:${REDIS_PORT}:6379 redis:7-alpine

Or use shared infra:
  cd ../infra && docker compose up -d redis

Ensure Encore local secret RedisURL points to the same host:
  printf '%s' 'redis://${REDIS_HOST}:${REDIS_PORT}' | encore secret set --type local RedisURL
EOF
  exit 1
fi

# Re-use wabantu-infra redis if it is running but bound to the default port.
if docker ps --format '{{.Names}}' 2>/dev/null | grep -qx wabantu-redis; then
  echo "Found wabantu-redis container; waiting for redis://${REDIS_HOST}:${REDIS_PORT}..."
  if wait_for_redis; then
    echo "Redis OK at redis://${REDIS_HOST}:${REDIS_PORT} (wabantu-redis)"
    exit 0
  fi
fi

start_test_container
if ! wait_for_redis; then
  echo "error: Redis still unreachable at redis://${REDIS_HOST}:${REDIS_PORT} after starting ${CONTAINER}" >&2
  exit 1
fi

echo "Redis OK at redis://${REDIS_HOST}:${REDIS_PORT} (${CONTAINER})"
