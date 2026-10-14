#!/usr/bin/env bash
# Artifact Registry, service account, IAM, dan 14 topik Pub/Sub.
# Tidak membuat subscription push dan tidak men-deploy Cloud Run.
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
SA="${RUNTIME_SA:-wabantu-cloudrun}"
SA_EMAIL="${SA}@${PROJECT}.iam.gserviceaccount.com"

gcloud config set project "$PROJECT"

if ! gcloud artifacts repositories describe "$REPO" --location="$REGION" >/dev/null 2>&1; then
  gcloud artifacts repositories create "$REPO" \
    --repository-format=docker \
    --location="$REGION" \
    --description="WABantu lomba api-go"
fi

if ! gcloud iam service-accounts describe "$SA_EMAIL" >/dev/null 2>&1; then
  gcloud iam service-accounts create "$SA" --display-name="WABantu Cloud Run lomba"
fi

for role in roles/pubsub.publisher roles/secretmanager.secretAccessor; do
  gcloud projects add-iam-policy-binding "$PROJECT" \
    --member="serviceAccount:${SA_EMAIL}" \
    --role="$role" \
    --condition=None >/dev/null
done

PROJECT_NUMBER="$(gcloud projects describe "$PROJECT" --format='value(projectNumber)')"
PUBSUB_AGENT="service-${PROJECT_NUMBER}@gcp-sa-pubsub.iam.gserviceaccount.com"
gcloud iam service-accounts add-iam-policy-binding "$SA_EMAIL" \
  --member="serviceAccount:${PUBSUB_AGENT}" \
  --role="roles/iam.serviceAccountTokenCreator" >/dev/null

python3 - "$PROJECT" <<'PY'
import json, subprocess, sys
project = sys.argv[1]
rows = json.load(open("deploy/cloudrun/pubsub.json"))
for row in rows:
    name = f"lomba-{row['topic']}"
    describe = subprocess.run(
        ["gcloud", "pubsub", "topics", "describe", name, f"--project={project}"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    if describe.returncode != 0:
        subprocess.check_call(
            ["gcloud", "pubsub", "topics", "create", name, f"--project={project}"]
        )
        print(f"created topic {name}")
    else:
        print(f"topic exists {name}")
PY

echo "sa=$SA_EMAIL"
echo "registry=${REGION}-docker.pkg.dev/${PROJECT}/${REPO}"
echo "Subscription push dibuat oleh deploy.sh setelah URL Cloud Run ada."
