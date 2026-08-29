#!/usr/bin/env bash
# Import secrets from ../api/.env into Encore local secrets.
#
# Prerequisites (once per machine):
#   1. encore auth login
#   2. encore app init    # registers this repo on Encore Cloud (fixes app_not_found)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${ROOT}/../api/.env"

if [[ ! -f "$ENV_FILE" ]]; then
  echo "Missing $ENV_FILE — copy from api/.env.example first." >&2
  exit 1
fi

# Read KEY=VALUE from .env without sourcing (values may contain spaces).
env_get() {
  local key="$1"
  local line value
  line="$(grep -E "^[[:space:]]*${key}=" "$ENV_FILE" | tail -n 1 || true)"
  if [[ -z "$line" ]]; then
    return 1
  fi
  value="${line#*=}"
  # Strip optional surrounding quotes.
  if [[ "$value" =~ ^\".*\"$ ]]; then
    value="${value:1:${#value}-2}"
  elif [[ "$value" =~ ^\'.*\'$ ]]; then
    value="${value:1:${#value}-2}"
  fi
  printf '%s' "$value"
}

set_secret() {
  local name="$1"
  local value="$2"
  if [[ -z "${value// }" ]]; then
    echo "skip $name (empty)"
    return
  fi
  printf '%s' "$value" | encore secret set --type local "$name"
  echo "ok $name"
}

cd "$ROOT"

JWT_ACCESS_SECRET="$(env_get JWT_ACCESS_SECRET || true)"
DATA_ENCRYPTION_KEY="$(env_get DATA_ENCRYPTION_KEY || true)"
REDIS_HOST="$(env_get REDIS_HOST || true)"
REDIS_PORT="$(env_get REDIS_PORT || true)"
ANTHROPIC_API_KEY="$(env_get ANTHROPIC_API_KEY || true)"
AI_INTERNAL_TOKEN="$(env_get AI_INTERNAL_TOKEN || true)"
META_WEBHOOK_VERIFY_TOKEN="$(env_get META_WEBHOOK_VERIFY_TOKEN || true)"
MIDTRANS_SERVER_KEY="$(env_get MIDTRANS_SERVER_KEY || true)"
MIDTRANS_CLIENT_KEY="$(env_get MIDTRANS_CLIENT_KEY || true)"
MIDTRANS_IS_PRODUCTION="$(env_get MIDTRANS_IS_PRODUCTION || true)"
RAJAONGKIR_API_KEY="$(env_get RAJAONGKIR_API_KEY || true)"
RAJAONGKIR_ACCOUNT_TYPE="$(env_get RAJAONGKIR_ACCOUNT_TYPE || true)"
SENTRY_DSN="$(env_get SENTRY_DSN || true)"

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"

set_secret JWTSecret "$JWT_ACCESS_SECRET"
set_secret DataEncryptionKey "$DATA_ENCRYPTION_KEY"
set_secret RedisURL "redis://${REDIS_HOST}:${REDIS_PORT}"
ANTHROPIC_MODEL="$(env_get ANTHROPIC_MODEL || true)"
ANTHROPIC_MAX_TOKENS="$(env_get ANTHROPIC_MAX_TOKENS || true)"

set_secret AnthropicApiKey "$ANTHROPIC_API_KEY"
set_secret AnthropicAPIKey "$ANTHROPIC_API_KEY"
# Model / max tokens use code defaults (see ai/api.go). Set these only if you add them back to secrets struct.
# set_secret AnthropicModel "${ANTHROPIC_MODEL:-claude-sonnet-4-5}"
# set_secret AnthropicMaxToks "${ANTHROPIC_MAX_TOKENS:-1024}"
set_secret AiInternalToken "$AI_INTERNAL_TOKEN"
set_secret WebhookVerifyToken "$META_WEBHOOK_VERIFY_TOKEN"
set_secret MidtransServerKey "$MIDTRANS_SERVER_KEY"
set_secret MidtransClientKey "$MIDTRANS_CLIENT_KEY"
set_secret MidtransIsProduction "${MIDTRANS_IS_PRODUCTION:-false}"
set_secret RajaOngkirAPIKey "$RAJAONGKIR_API_KEY"
set_secret RajaOngkirAccountType "${RAJAONGKIR_ACCOUNT_TYPE:-starter}"
set_secret SentryDSN "$SENTRY_DSN"

# Non-secret env vars for api-go (codesim reads via .env.local on encore run)
sync_api_go_env_local() {
  local dest="${ROOT}/.env.local"
  local codesim ai_key
  codesim="$(env_get CODESIM_LIVE_AI_GEN || true)"
  ai_key="$(env_get ANTHROPIC_API_KEY || true)"

  {
    echo "# Auto-synced from api/.env by setup-secrets-from-env.sh"
    echo "# Do not commit. Re-run script after changing api/.env"
    echo ""
    if [[ -n "${codesim// }" ]]; then
      echo "CODESIM_LIVE_AI_GEN=${codesim}"
    else
      echo "# CODESIM_LIVE_AI_GEN=1"
    fi
    if [[ -n "${ai_key// }" ]]; then
      echo "ANTHROPIC_API_KEY=${ai_key}"
    fi
  } >"$dest"
  echo "ok ${dest} (CODESIM_LIVE_AI_GEN + ANTHROPIC_API_KEY fallback)"
}

sync_api_go_env_local

echo "Done. Run: encore secret list"
echo "Restart encore run so codesim picks up .env.local"
