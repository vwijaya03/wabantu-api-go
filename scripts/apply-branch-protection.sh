#!/usr/bin/env bash
# Terapkan branch protection ke default branch (master) via GitHub API.
# Butuh: gh CLI + gh auth login
#
# Usage:
#   ./scripts/apply-branch-protection.sh
#   ./scripts/apply-branch-protection.sh vwijaya03/wabantu-api-go main

set -euo pipefail

REPO="${1:-vwijaya03/wabantu-api-go}"
BRANCH="${2:-master}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "Applying branch protection to ${REPO} branch ${BRANCH}..."
gh api --method PUT "repos/${REPO}/branches/${BRANCH}/protection" \
  --input "${SCRIPT_DIR}/github-branch-protection-master.json"
echo "Done."
