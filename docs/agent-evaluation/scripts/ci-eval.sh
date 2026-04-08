#!/usr/bin/env bash
# CI-friendly agent eval: scenarios 01+02 with heuristic + LLM judge (Success criteria from each scenario .md).
#
# Required secrets (e.g. GitHub Actions): ANTHROPIC_API_KEY, EVAL_TEST_DESTINATION_URL
# Optional: same vars in docs/agent-evaluation/.env for local runs.
#
# Scenarios: 01 = curl quickstart shape; 02 = TypeScript SDK script. See README § CI.
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

exec npm run eval:ci
