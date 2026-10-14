#!/usr/bin/env bash
# Build image Encore untuk Cloud Run (linux/amd64). Tidak men-deploy.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$ROOT"

if [[ -f deploy/cloudrun/.env ]]; then
  set -a
  # shellcheck disable=SC1091
  source deploy/cloudrun/.env
  set +a
fi

python3 scripts/cloudrun/render_infra_config.py

PROJECT="${GCP_PROJECT:?GCP_PROJECT}"
REGION="${GCP_REGION:-asia-southeast2}"
REPO="${AR_REPO:-wabantu}"
IMAGE="${REGION}-docker.pkg.dev/${PROJECT}/${REPO}/api:${IMAGE_TAG:-release-cr}"

encore build docker \
  --config deploy/cloudrun/infra.config.json \
  --arch amd64 \
  "$IMAGE"

echo "image=$IMAGE"
echo "Push menyusul di deploy.sh. Config ter-bake; ganti CLOUD_RUN_URL lalu build ulang."
