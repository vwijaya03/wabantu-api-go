#!/usr/bin/env bash
# Import secrets from ../api/.env into Encore secrets.
#
# Usage:
#   ./scripts/setup-secrets-from-env.sh              # --type local (encore run)
#   ./scripts/setup-secrets-from-env.sh --env staging # Encore Cloud env (staging, prod, …)
#
# Untuk cloud + RedisURL Upstash, lebih lengkap: ./scripts/setup-secrets-for-cloud.sh staging
#
# Key mappings (api/.env → Encore secret):
#   JWT_ACCESS_SECRET          → JWTSecret
#   DATA_ENCRYPTION_KEY        → DataEncryptionKey
#   META_WEBHOOK_VERIFY_TOKEN  → WebhookVerifyToken  (Meta webhook GET verify)
#   OPENAI_API_KEY             → OpenAIApiKey
#   PINECONE_API_KEY           → PineconeApiKey
#   PINECONE_INDEX_HOST        → PineconeIndexHost
#   ...
#
# Prerequisites (once per machine):
#   1. encore auth login
#   2. encore app init    # registers this repo on Encore Cloud (fixes app_not_found)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${ROOT}/../api/.env"
SECRET_TARGET_TYPE="local"
SECRET_TARGET_ENV=""

usage() {
  echo "Usage: $0 [--env <encore-env-name>]" >&2
  echo "  default: encore secret set --type local (local dev)" >&2
  echo "  --env staging: encore secret set --env=staging (cloud)" >&2
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --env)
      [[ $# -ge 2 ]] || { usage; exit 1; }
      SECRET_TARGET_ENV="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage
      exit 1
      ;;
  esac
done

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
  local attempt
  if [[ -z "${value// }" ]]; then
    echo "skip $name (empty)"
    return 0
  fi
  local target_label
  if [[ -n "$SECRET_TARGET_ENV" ]]; then
    target_label="env=$SECRET_TARGET_ENV"
  else
    target_label="type=$SECRET_TARGET_TYPE"
  fi
  for attempt in 1 2 3 4 5; do
    if [[ -n "$SECRET_TARGET_ENV" ]]; then
      if printf '%s' "$value" | encore secret set --env="$SECRET_TARGET_ENV" "$name"; then
        echo "ok $name → $target_label"
        return 0
      fi
    elif printf '%s' "$value" | encore secret set --type "$SECRET_TARGET_TYPE" "$name"; then
      echo "ok $name → $target_label"
      return 0
    fi
    echo "retry $name ($attempt/5) — encore cloud timeout, waiting..." >&2
    sleep 3
  done
  echo "FAILED $name after 5 attempts (check network / encore auth login)" >&2
  return 1
}

normalize_pinecone_host() {
  local h="$1"
  h="${h#https://}"
  h="${h#http://}"
  h="${h%/}"
  printf '%s' "$h"
}

cd "$ROOT"

JWT_ACCESS_SECRET="$(env_get JWT_ACCESS_SECRET || true)"
DATA_ENCRYPTION_KEY="$(env_get DATA_ENCRYPTION_KEY || true)"
REDIS_HOST="$(env_get REDIS_HOST || true)"
REDIS_PORT="$(env_get REDIS_PORT || true)"
ANTHROPIC_API_KEY="$(env_get ANTHROPIC_API_KEY || true)"
AI_INTERNAL_TOKEN="$(env_get AI_INTERNAL_TOKEN || true)"
META_WEBHOOK_VERIFY_TOKEN="$(env_get META_WEBHOOK_VERIFY_TOKEN || true)"
WEBHOOK_VERIFY_TOKEN="$(env_get WEBHOOK_VERIFY_TOKEN || true)"
MIDTRANS_SERVER_KEY="$(env_get MIDTRANS_SERVER_KEY || true)"
MIDTRANS_CLIENT_KEY="$(env_get MIDTRANS_CLIENT_KEY || true)"
MIDTRANS_IS_PRODUCTION="$(env_get MIDTRANS_IS_PRODUCTION || true)"
RAJAONGKIR_API_KEY="$(env_get RAJAONGKIR_API_KEY || true)"
RAJAONGKIR_ACCOUNT_TYPE="$(env_get RAJAONGKIR_ACCOUNT_TYPE || true)"
SENTRY_DSN="$(env_get SENTRY_DSN || true)"
OPENAI_API_KEY="$(env_get OPENAI_API_KEY || true)"
PINECONE_API_KEY="$(env_get PINECONE_API_KEY || true)"
PINECONE_INDEX_HOST="$(env_get PINECONE_INDEX_HOST || true)"
REDIS_URL_DIRECT="$(env_get REDIS_URL || true)"

REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"

set_secret JWTSecret "$JWT_ACCESS_SECRET"
set_secret DataEncryptionKey "$DATA_ENCRYPTION_KEY"
if [[ -n "${REDIS_URL_DIRECT// }" ]]; then
  set_secret RedisURL "$REDIS_URL_DIRECT"
else
  set_secret RedisURL "redis://${REDIS_HOST}:${REDIS_PORT}"
fi
ANTHROPIC_MODEL="$(env_get ANTHROPIC_MODEL || true)"
ANTHROPIC_MAX_TOKENS="$(env_get ANTHROPIC_MAX_TOKENS || true)"

set_secret AnthropicAPIKey "$ANTHROPIC_API_KEY"
# Model / max tokens use code defaults (see ai/api.go). Set these only if you add them back to secrets struct.
# set_secret AnthropicModel "${ANTHROPIC_MODEL:-claude-sonnet-4-5}"
# set_secret AnthropicMaxToks "${ANTHROPIC_MAX_TOKENS:-1024}"
set_secret AiInternalToken "$AI_INTERNAL_TOKEN"

# WhatsApp / Meta webhook (must match Verify token in Meta Developer Console)
WEBHOOK_VERIFY_VALUE="${META_WEBHOOK_VERIFY_TOKEN:-$WEBHOOK_VERIFY_TOKEN}"
set_secret WebhookVerifyToken "$WEBHOOK_VERIFY_VALUE"

set_secret MidtransServerKey "$MIDTRANS_SERVER_KEY"
set_secret MidtransClientKey "$MIDTRANS_CLIENT_KEY"
set_secret MidtransIsProduction "${MIDTRANS_IS_PRODUCTION:-false}"
set_secret RajaOngkirAPIKey "$RAJAONGKIR_API_KEY"
set_secret RajaOngkirAccountType "${RAJAONGKIR_ACCOUNT_TYPE:-starter}"
set_secret SentryDSN "$SENTRY_DSN"

# RAG (Pinecone + OpenAI embeddings) — used by shared/retrieval (PR1+)
set_secret OpenAIApiKey "$OPENAI_API_KEY"
set_secret PineconeApiKey "$PINECONE_API_KEY"
set_secret PineconeIndexHost "$(normalize_pinecone_host "$PINECONE_INDEX_HOST")"

if [[ -n "$SECRET_TARGET_ENV" ]]; then
  echo "Done. Run: encore secret list --env=$SECRET_TARGET_ENV"
else
  echo "Done. Run: encore secret list"
fi
