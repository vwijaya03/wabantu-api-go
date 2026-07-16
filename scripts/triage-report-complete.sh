#!/usr/bin/env bash
# POST job completion to Encore internal API with retries.
# Usage: triage-report-complete.sh <job_id> <json_body>
set -euo pipefail

JOB_ID="${1:?job id required}"
BODY="${2:?json body required}"

if [ -z "${ENCORE_API_URL:-}" ] || [ -z "${AI_INTERNAL_TOKEN:-}" ]; then
  echo "ENCORE_API_URL and AI_INTERNAL_TOKEN required" >&2
  exit 1
fi

URL="${ENCORE_API_URL}/api/v1/internal/ai-triage/jobs/${JOB_ID}/complete"

for attempt in 1 2 3; do
  if curl -fsS -X POST \
    -H "Content-Type: application/json" \
    -H "X-Ai-Internal-Token: ${AI_INTERNAL_TOKEN}" \
    -d "$BODY" \
    "$URL"; then
    echo ""
    echo "triage complete reported (attempt $attempt)"
    exit 0
  fi
  echo "triage complete callback failed (attempt $attempt)" >&2
  sleep 2
done

echo "triage complete callback failed after 3 attempts" >&2
exit 1
