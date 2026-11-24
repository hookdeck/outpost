#!/usr/bin/env bash
# CI-friendly agent eval: scenarios 01+02 with heuristic + LLM judge (Success criteria from each scenario .md).
#
# Required secrets (e.g. GitHub Actions): ANTHROPIC_API_KEY, EVAL_TEST_DESTINATION_URL, OUTPOST_API_KEY
# Optional: same vars in docs/agent-evaluation/.env for local runs.
#
# Scenarios: 01 = curl quickstart shape; 02 = TypeScript SDK script. See README § CI.
# OUTPOST_API_KEY is forwarded to the agent sandbox so it can run smoke tests during eval:ci.
# After success, ./scripts/execute-ci-artifacts.sh re-runs the saved artifacts (CI step 2).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

if [[ -z "${ANTHROPIC_API_KEY:-}" ]]; then
  echo "ci-eval: ANTHROPIC_API_KEY is not set" >&2
  exit 1
fi
if [[ -z "${EVAL_TEST_DESTINATION_URL:-}" ]]; then
  echo "ci-eval: EVAL_TEST_DESTINATION_URL is not set" >&2
  exit 1
fi
if [[ -z "${OUTPOST_API_KEY:-}" ]]; then
  echo "ci-eval: OUTPOST_API_KEY is not set (required so the agent can run live Outpost smoke tests)" >&2
  exit 1
fi

export OUTPOST_TEST_WEBHOOK_URL="${OUTPOST_TEST_WEBHOOK_URL:-${EVAL_TEST_DESTINATION_URL:-}}"
: "${OUTPOST_API_BASE_URL:=https://api.outpost.hookdeck.com/2025-07-01}"
export OUTPOST_API_BASE_URL OUTPOST_TEST_WEBHOOK_URL

exec npm run eval:ci
