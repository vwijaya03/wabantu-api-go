#!/usr/bin/env bash
# Deploy image yang sudah di-build. Maksimal 2 instance.
# Cron Encore tidak dipasang (finance-recurring, reset-monthly-usage).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [[ -f deploy/cloudrun/.env ]]; then
  set -a
  # shellcheck disable=SC1091
  source deploy/cloudrun/.env
  set +a
fi

PROJECT="${GCP_PROJECT:?GCP_PROJECT}"
REGION="${GCP_REGION:-asia-southeast2}"
REPO="${AR_REPO:-wabantu}"
SERVICE="${SERVICE_NAME:-wabantu-api}"
SA="${RUNTIME_SA:-wabantu-cloudrun}"
SA_EMAIL="${SA}@${PROJECT}.iam.gserviceaccount.com"
IMAGE="${REGION}-docker.pkg.dev/${PROJECT}/${REPO}/api:${IMAGE_TAG:-release-cr}"

gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet
docker push "$IMAGE"

# Nama secret Secret Manager = nama env. Semua wajib ada sebelum deploy.
SECRETS=(
  JWTSecret DataEncryptionKey RedisURL PlatformAdminBootstrapKey
  WebhookVerifyToken AnthropicAPIKey AiInternalToken
  GitHubActionsToken AiTriageInternalToken
  MidtransServerKey MidtransClientKey MidtransIsProduction
  OpenAIApiKey PineconeApiKey PineconeIndexHost RetrievalBudgetMs
  RajaOngkirAPIKey RajaOngkirAccountType
  AWSS3Bucket AWSS3Region AWSS3AccessKeyID AWSS3SecretAccessKey
  SentryDSN
  ENCORE_SERVICE_AUTH_KEY SQL_PASSWORD REDIS_AUTH
  GeminiAPIKey SimulatorTenantAllowlist
  FIRESTORE_PROJECT_ID FIREBASE_SERVICE_ACCOUNT_JSON
)
SET_SECRETS=""
for name in "${SECRETS[@]}"; do
  SET_SECRETS+="${name}=${name}:latest,"
done
SET_SECRETS="${SET_SECRETS%,}"

gcloud run deploy "$SERVICE" \
  --project="$PROJECT" \
  --region="$REGION" \
  --image="$IMAGE" \
  --service-account="$SA_EMAIL" \
  --port=8080 \
  --cpu=1 \
  --memory=1Gi \
  --min-instances=0 \
  --max-instances=2 \
  --concurrency=40 \
  --timeout=300 \
  --allow-unauthenticated \
  --set-secrets="$SET_SECRETS"

URL="$(gcloud run services describe "$SERVICE" --project="$PROJECT" --region="$REGION" --format='value(status.url)')"
echo "url=$URL"

if [[ "${CLOUD_RUN_URL:-}" != "$URL" ]]; then
  echo "CLOUD_RUN_URL di deploy/cloudrun/.env masih bukan URL ini."
  echo "Isi CLOUD_RUN_URL=$URL lalu jalankan build-image.sh dan deploy.sh sekali lagi supaya push Pub/Sub mengarah ke service ini."
fi

python3 - "$PROJECT" "$URL" "$SA_EMAIL" <<'PY'
import json, subprocess, sys
project, base, sa = sys.argv[1:]
base = base.rstrip("/")
rows = json.load(open("deploy/cloudrun/pubsub.json"))
for row in rows:
    sub = row.get("subscription")
    if not sub:
        continue
    topic = f"lomba-{row['topic']}"
    name = f"lomba-{sub}"
    endpoint = f"{base}/__encore/pubsub/push/{sub}"
    common = [
        f"--topic={topic}",
        f"--push-endpoint={endpoint}",
        f"--push-auth-service-account={sa}",
        f"--push-auth-token-audience={base}",
        f"--project={project}",
    ]
    describe = subprocess.run(
        ["gcloud", "pubsub", "subscriptions", "describe", name, f"--project={project}"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    if describe.returncode != 0:
        subprocess.check_call(["gcloud", "pubsub", "subscriptions", "create", name, *common])
        print(f"created subscription {name}")
    else:
        subprocess.check_call(["gcloud", "pubsub", "subscriptions", "update", name, *common[1:]])
        print(f"updated subscription {name}")
PY
