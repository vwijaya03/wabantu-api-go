#!/usr/bin/env bash
# Copy secrets from ../api/.env into Encore Cloud dev environments (staging, etc.).
#
# Usage:
#   ./scripts/setup-secrets-for-cloud.sh staging
#   REDIS_URL='rediss://...' ./scripts/setup-secrets-for-cloud.sh staging
#
# Redis on cloud cannot be localhost — set REDIS_URL to Upstash/Railway/etc. before running,
# or pass it inline as shown above.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_NAME="${1:?Usage: $0 <encore-env-name> (e.g. staging)}"
ENV_FILE="${ROOT}/../api/.env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "Missing $ENV_FILE" >&2
  exit 1
fi

env_get() {
  local key="$1"
  local line value
  line="$(grep -E "^[[:space:]]*${key}=" "$ENV_FILE" | tail -n 1 || true)"
  [[ -z "$line" ]] && return 1
  value="${line#*=}"
  if [[ "$value" =~ ^\".*\"$ ]]; then value="${value:1:${#value}-2}"; fi
  if [[ "$value" =~ ^\'.*\'$ ]]; then value="${value:1:${#value}-2}"; fi
  printf '%s' "$value"
}

set_cloud_secret() {
  local name="$1"
  local value="$2"
  if [[ -z "${value// }" ]]; then
    echo "skip $name (empty)"
    return
  fi
  printf '%s' "$value" | encore secret set --env="$ENV_NAME" "$name"
  echo "ok $name → env=$ENV_NAME"
}

cd "$ROOT"

REDIS_URL="${REDIS_URL:-$(env_get REDIS_URL 2>/dev/null || true)}"
if [[ -z "$REDIS_URL" || "$REDIS_URL" == redis://localhost:* || "$REDIS_URL" == redis://127.0.0.1:* ]]; then
  echo "ERROR: Set REDIS_URL to a cloud Redis before deploy (e.g. Upstash free tier)." >&2
  echo "  REDIS_URL='rediss://default:...@....upstash.io:6379' $0 $ENV_NAME" >&2
  exit 1
fi

JWT_ACCESS_SECRET="$(env_get JWT_ACCESS_SECRET || true)"
DATA_ENCRYPTION_KEY="$(env_get DATA_ENCRYPTION_KEY || true)"
ANTHROPIC_API_KEY="$(env_get ANTHROPIC_API_KEY || true)"
AI_INTERNAL_TOKEN="$(env_get AI_INTERNAL_TOKEN || true)"
META_WEBHOOK_VERIFY_TOKEN="$(env_get META_WEBHOOK_VERIFY_TOKEN || true)"
MIDTRANS_SERVER_KEY="$(env_get MIDTRANS_SERVER_KEY || true)"
MIDTRANS_CLIENT_KEY="$(env_get MIDTRANS_CLIENT_KEY || true)"
MIDTRANS_IS_PRODUCTION="$(env_get MIDTRANS_IS_PRODUCTION || true)"
RAJAONGKIR_API_KEY="$(env_get RAJAONGKIR_API_KEY || true)"
RAJAONGKIR_ACCOUNT_TYPE="$(env_get RAJAONGKIR_ACCOUNT_TYPE || true)"
SENTRY_DSN="$(env_get SENTRY_DSN || true)"
PLATFORM_ADMIN_BOOTSTRAP="$(env_get PLATFORM_ADMIN_BOOTSTRAP_SECRET || true)"

set_cloud_secret JWTSecret "$JWT_ACCESS_SECRET"
set_cloud_secret DataEncryptionKey "$DATA_ENCRYPTION_KEY"
set_cloud_secret RedisURL "$REDIS_URL"
set_cloud_secret AnthropicApiKey "$ANTHROPIC_API_KEY"
set_cloud_secret AnthropicAPIKey "$ANTHROPIC_API_KEY"
set_cloud_secret AiInternalToken "$AI_INTERNAL_TOKEN"
set_cloud_secret WebhookVerifyToken "$META_WEBHOOK_VERIFY_TOKEN"
set_cloud_secret MidtransServerKey "$MIDTRANS_SERVER_KEY"
set_cloud_secret MidtransClientKey "$MIDTRANS_CLIENT_KEY"
set_cloud_secret MidtransIsProduction "${MIDTRANS_IS_PRODUCTION:-false}"
set_cloud_secret RajaOngkirApiKey "$RAJAONGKIR_API_KEY"
set_cloud_secret RajaOngkirAccountType "${RAJAONGKIR_ACCOUNT_TYPE:-starter}"
set_cloud_secret SentryDSN "$SENTRY_DSN"
set_cloud_secret PlatformAdminBootstrapSecret "$PLATFORM_ADMIN_BOOTSTRAP"

echo "Done. Verify: encore secret list --env=$ENV_NAME"
