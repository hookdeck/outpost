#!/usr/bin/env bash
# After a successful eval:ci (same ISO stamp for scenario-01 and scenario-02), run generated
# curl script and TypeScript quickstart against live Outpost (tenant → destination → publish).
#
# Required env: OUTPOST_API_KEY, OUTPOST_TEST_WEBHOOK_URL (often same URL as EVAL_TEST_DESTINATION_URL)
# Optional: OUTPOST_API_BASE_URL (managed default if unset)
# Optional: OUTPOST_CI_CLEANUP_TENANT — tenant id to DELETE before and after this script (EXIT trap).
#   CI sets this so repeated runs do not accumulate destinations on a shared eval tenant; pair with
#   workflow concurrency so two jobs never delete the same tenant mid-flight.
# Optional: OUTPOST_SKIP_MANAGED_TOPICS_VERIFY=1 — skip GET /configs topic allowlist check.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNS="$ROOT/results/runs"

if [[ -z "${OUTPOST_API_KEY:-}" ]]; then
  echo "execute-ci-artifacts: OUTPOST_API_KEY is not set" >&2
  exit 1
fi
export OUTPOST_TEST_WEBHOOK_URL="${OUTPOST_TEST_WEBHOOK_URL:-${EVAL_TEST_DESTINATION_URL:-}}"
if [[ -z "${OUTPOST_TEST_WEBHOOK_URL:-}" ]]; then
  echo "execute-ci-artifacts: OUTPOST_TEST_WEBHOOK_URL or EVAL_TEST_DESTINATION_URL must be set" >&2
  exit 1
fi

# Managed API default (agent-generated scripts often expect this in the environment).
# Use := so empty string from .env is treated like unset (otherwise curl hits /tenants without /2025-07-01 → 404).
: "${OUTPOST_API_BASE_URL:=https://api.outpost.hookdeck.com/2025-07-01}"
export OUTPOST_API_BASE_URL

# Managed-only: if GET /configs succeeds, TOPICS must be empty (any topic), include "*", or list
# OUTPOST_CI_PUBLISH_TOPIC (default user.created). Non-200 skips (e.g. self-hosted has no /configs).
verify_managed_topics_allow_ci_publish() {
  if [[ -n "${OUTPOST_SKIP_MANAGED_TOPICS_VERIFY:-}" ]]; then
    echo "execute-ci-artifacts: skipping managed TOPICS verify (OUTPOST_SKIP_MANAGED_TOPICS_VERIFY set)" >&2
    return 0
  fi
  local topic="${OUTPOST_CI_PUBLISH_TOPIC:-user.created}"
  local base="${OUTPOST_API_BASE_URL%/}"
  local tmp code
  tmp="$(mktemp)"
  code="$(
    curl -sS -o "$tmp" -w "%{http_code}" -H "Authorization: Bearer $OUTPOST_API_KEY" \
      "$base/configs" || echo "000"
  )"
  if [[ "$code" != "200" ]]; then
    echo "execute-ci-artifacts: skip managed TOPICS verify (GET /configs HTTP $code)" >&2
    rm -f "$tmp"
    return 0
  fi
  export _EXEC_CI_TOPICS_JSON="$tmp"
  export _EXEC_CI_PUBLISH_TOPIC="$topic"
  if ! python3 <<'PY'
import json
import os
import sys

path = os.environ["_EXEC_CI_TOPICS_JSON"]
need = os.environ["_EXEC_CI_PUBLISH_TOPIC"]
with open(path, encoding="utf-8") as f:
    data = json.load(f)
raw = (data.get("TOPICS") or "").strip()
if not raw:
    print(
        "execute-ci-artifacts: managed TOPICS empty — publish topic unrestricted for this check",
        file=sys.stderr,
    )
    sys.exit(0)
parts = [p.strip() for p in raw.split(",") if p.strip()]
if "*" in parts:
    print("execute-ci-artifacts: managed TOPICS includes '*'", file=sys.stderr)
    sys.exit(0)
if need in parts:
    print(f"execute-ci-artifacts: managed TOPICS includes {need!r}", file=sys.stderr)
    sys.exit(0)
print(
    f"execute-ci-artifacts: managed TOPICS={parts!r} does not include publish topic {need!r} (or '*'). "
    "Set topics in project settings, PUT /configs, or set OUTPOST_SKIP_MANAGED_TOPICS_VERIFY=1.",
    file=sys.stderr,
)
sys.exit(1)
PY
  then
    unset _EXEC_CI_TOPICS_JSON _EXEC_CI_PUBLISH_TOPIC
    rm -f "$tmp"
    return 1
  fi
  unset _EXEC_CI_TOPICS_JSON _EXEC_CI_PUBLISH_TOPIC
  rm -f "$tmp"
}

verify_managed_topics_allow_ci_publish

# Delete shared CI tenant (200) or no-op (404). Does not fail the script on 200/404.
outpost_ci_delete_cleanup_tenant() {
  local tid=${OUTPOST_CI_CLEANUP_TENANT:-}
  [[ -n "$tid" ]] || return 0
  local base="${OUTPOST_API_BASE_URL%/}"
  local code
  code="$(
    curl -sS -o /dev/null -w "%{http_code}" -X DELETE \
      "$base/tenants/$tid" \
      -H "Authorization: Bearer $OUTPOST_API_KEY" || echo "000"
  )"
  if [[ "$code" == "200" || "$code" == "404" ]]; then
    echo "execute-ci-artifacts: tenant cleanup $tid (DELETE HTTP $code)" >&2
  else
    echo "execute-ci-artifacts: warning DELETE tenant $tid returned HTTP $code (expected 200 or 404)" >&2
  fi
}

if [[ -n "${OUTPOST_CI_CLEANUP_TENANT:-}" ]]; then
  outpost_ci_delete_cleanup_tenant
  trap outpost_ci_delete_cleanup_tenant EXIT
fi

if [[ ! -d "$RUNS" ]]; then
  echo "execute-ci-artifacts: missing $RUNS (run eval:ci first)" >&2
  exit 1
fi

# Latest scenario-01 run directory by mtime (same batch shares stamp with scenario-02).
d01=""
best=0
for d in "$RUNS"/*-scenario-01; do
  [[ -d "$d" ]] || continue
  m=$(stat -c %Y "$d" 2>/dev/null || stat -f %m "$d")
  if (( m >= best )); then
    best=$m
    d01=$d
  fi
done

if [[ -z "$d01" ]]; then
  echo "execute-ci-artifacts: no *-scenario-01 directory under $RUNS" >&2
  exit 1
fi

prefix=${d01%-scenario-01}
d02="${prefix}-scenario-02"
if [[ ! -d "$d02" ]]; then
  echo "execute-ci-artifacts: expected paired run dir missing: $d02" >&2
  exit 1
fi

pick_sh() {
  local dir=$1 f
  for f in "$dir"/*quickstart*.sh "$dir"/outpost*.sh; do
    [[ -f "$f" ]] && { echo "$f"; return 0; }
  done
  for f in "$dir"/*.sh; do
    [[ -f "$f" ]] && { echo "$f"; return 0; }
  done
  return 1
}

pick_ts() {
  local dir=$1 f
  for f in "$dir"/outpost-quickstart.ts "$dir"/*quickstart*.ts; do
    [[ -f "$f" ]] && { echo "$f"; return 0; }
  done
  for f in "$dir"/*.ts; do
    [[ -f "$f" ]] && { echo "$f"; return 0; }
  done
  return 1
}

# Agent-generated scripts may `source .env` which can overwrite CI-exported
# variables with placeholders.  Hide any agent-written .env during execution
# so the real CI env vars are the ones the child process sees.
hide_agent_env() {
  local dir=$1
  local env_path="$dir/.env"
  local stash_path="$dir/.env.ci-stash.$$"
  if [[ -f "$env_path" ]]; then
    mv "$env_path" "$stash_path"
    echo "$stash_path"
  fi
}

restore_agent_env() {
  local dir=$1 stash_path="${2:-}"
  if [[ -n "$stash_path" && -f "$stash_path" ]]; then
    mv "$stash_path" "$dir/.env"
  fi
}

echo "execute-ci-artifacts: scenario 01 dir=$d01"
sh_path=$(pick_sh "$d01") || {
  echo "execute-ci-artifacts: no .sh script found in $d01" >&2
  exit 1
}
echo "execute-ci-artifacts: running bash $sh_path"
export OUTPOST_API_KEY OUTPOST_TEST_WEBHOOK_URL
[[ -n "${OUTPOST_API_BASE_URL:-}" ]] && export OUTPOST_API_BASE_URL
chmod +x "$sh_path" 2>/dev/null || true
# Run from the scenario 01 run dir so relative paths in the generated script behave.
cd "$d01"
stash01="$(hide_agent_env "$d01")"
bash "$sh_path" || {
  restore_agent_env "$d01" "$stash01"
  echo "execute-ci-artifacts: scenario 01 shell failed (curl exit 22 = HTTP error). 404 is often a wrong path or a publish/destination topic that is not configured in your Outpost project. Set OUTPOST_API_BASE_URL if needed; try npm run smoke:execute-ci (uses destination topics [\"*\"])." >&2
  exit 1
}
restore_agent_env "$d01" "$stash01"

echo "execute-ci-artifacts: scenario 02 dir=$d02"
ts_path=$(pick_ts "$d02") || {
  echo "execute-ci-artifacts: no .ts file found in $d02" >&2
  exit 1
}
echo "execute-ci-artifacts: running npx tsx $ts_path (from $d02)"
cd "$d02"
if [[ -f package.json ]]; then
  npm install --no-audit --no-fund
fi
stash02="$(hide_agent_env "$d02")"
export OUTPOST_API_KEY OUTPOST_TEST_WEBHOOK_URL
[[ -n "${OUTPOST_API_BASE_URL:-}" ]] && export OUTPOST_API_BASE_URL
npx --yes tsx "$ts_path" || {
  restore_agent_env "$d02" "$stash02"
  echo "execute-ci-artifacts: scenario 02 TypeScript failed. Check OUTPOST_API_KEY, OUTPOST_TEST_WEBHOOK_URL, and that OUTPOST_CI_PUBLISH_TOPIC (default user.created) exists in the project. Try: npm run smoke:execute-ci" >&2
  exit 1
}
restore_agent_env "$d02" "$stash02"

echo "execute-ci-artifacts: OK (scenario 01 shell + scenario 02 TypeScript)"
