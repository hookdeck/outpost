#!/usr/bin/env bash
# After a successful eval:ci (same ISO stamp for scenario-01 and scenario-02), run generated
# curl script and TypeScript quickstart against live Outpost (tenant → destination → publish).
#
# Required env: OUTPOST_API_KEY, OUTPOST_TEST_WEBHOOK_URL (often same URL as EVAL_TEST_DESTINATION_URL)
# Optional: OUTPOST_API_BASE_URL (managed default if unset)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
RUNS="$ROOT/results/runs"

if [[ -z "${OUTPOST_API_KEY:-}" ]]; then
  echo "execute-ci-artifacts: OUTPOST_API_KEY is not set" >&2
  exit 1
fi
if [[ -z "${OUTPOST_TEST_WEBHOOK_URL:-}" ]]; then
  echo "execute-ci-artifacts: OUTPOST_TEST_WEBHOOK_URL is not set" >&2
  exit 1
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
bash "$sh_path"

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
export OUTPOST_API_KEY OUTPOST_TEST_WEBHOOK_URL
[[ -n "${OUTPOST_API_BASE_URL:-}" ]] && export OUTPOST_API_BASE_URL
npx --yes tsx "$ts_path"

echo "execute-ci-artifacts: OK (scenario 01 shell + scenario 02 TypeScript)"
