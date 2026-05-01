#!/usr/bin/env bash
# Local / operator check for the same path as CI: materialize a fresh *-scenario-01 / *-scenario-02
# pair with hand-maintained scripts (wildcard destination topics), then run execute-ci-artifacts.sh.
#
# Requires: OUTPOST_API_KEY, OUTPOST_TEST_WEBHOOK_URL (source docs/agent-evaluation/.env or export)
# Optional: OUTPOST_API_BASE_URL, OUTPOST_CI_PUBLISH_TOPIC (default user.created — must exist in your project)
#
# Does not invoke the agent. Use this to verify secrets and managed API before relying on CI execution.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi
if [[ -f .env.ci ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env.ci
  set +a
fi

# Same as CI: webhook URL is often stored as EVAL_TEST_DESTINATION_URL in .env / .env.ci
export OUTPOST_TEST_WEBHOOK_URL="${OUTPOST_TEST_WEBHOOK_URL:-${EVAL_TEST_DESTINATION_URL:-}}"

if [[ -z "${OUTPOST_API_KEY:-}" || -z "${OUTPOST_TEST_WEBHOOK_URL:-}" ]]; then
  echo "smoke-test-execute-ci: set OUTPOST_API_KEY and OUTPOST_TEST_WEBHOOK_URL (or EVAL_TEST_DESTINATION_URL), e.g. source .env" >&2
  exit 1
fi

RUNS="$ROOT/results/runs"
mkdir -p "$RUNS"

STAMP="ci-smoke-$(date -u +%Y-%m-%dT%H-%M-%S)-$(printf '%03d' $((RANDOM % 1000)))Z"
d01="$RUNS/${STAMP}-scenario-01"
d02="$RUNS/${STAMP}-scenario-02"
mkdir -p "$d01" "$d02"

PUBLISH_TOPIC="${OUTPOST_CI_PUBLISH_TOPIC:-user.created}"

# Shell: managed API, unique tenant, destination topics * (no dashboard topic list required), then publish.
cat > "$d01/outpost_quickstart.sh" << 'EOSH'
#!/usr/bin/env bash
set -euo pipefail
BASE="${OUTPOST_API_BASE_URL:-https://api.outpost.hookdeck.com/2025-07-01}"
TENANT_ID="ci_smoke_${RANDOM}_$(date +%s)"
TOPIC="${OUTPOST_CI_PUBLISH_TOPIC:-user.created}"
DEST_JSON="$(OUTPOST_TEST_WEBHOOK_URL="$OUTPOST_TEST_WEBHOOK_URL" python3 -c '
import json, os
print(json.dumps({"type": "webhook", "topics": ["*"], "config": {"url": os.environ["OUTPOST_TEST_WEBHOOK_URL"]}}))
')"
curl -sS -f -X PUT "$BASE/tenants/$TENANT_ID" \
  -H "Authorization: Bearer $OUTPOST_API_KEY" -o /dev/null
curl -sS -f -X POST "$BASE/tenants/$TENANT_ID/destinations" \
  -H "Authorization: Bearer $OUTPOST_API_KEY" -H "Content-Type: application/json" \
  -d "$DEST_JSON" -o /dev/null
curl -sS -f -X POST "$BASE/publish" \
  -H "Authorization: Bearer $OUTPOST_API_KEY" -H "Content-Type: application/json" \
  -d "$(TENANT_ID="$TENANT_ID" TOPIC="$TOPIC" python3 -c '
import json, os
print(json.dumps({
  "tenant_id": os.environ["TENANT_ID"],
  "topic": os.environ["TOPIC"],
  "eligible_for_retry": True,
  "metadata": {"source": "ci-smoke-sh"},
  "data": {"smoke": True},
}))
')" -o /dev/null -w "publish_http=%{http_code}\n"
echo "smoke shell OK tenant=$TENANT_ID"
EOSH
chmod +x "$d01/outpost_quickstart.sh"

# TypeScript: same semantics (wildcard subscription); publish uses OUTPOST_CI_PUBLISH_TOPIC.
cat > "$d02/package.json" << 'EOJSON'
{
  "name": "ci-smoke-outpost-ts",
  "private": true,
  "type": "module",
  "dependencies": {
    "@hookdeck/outpost-sdk": "^0.9.0"
  }
}
EOJSON

cat > "$d02/outpost-quickstart.ts" << 'EOTS'
import { Outpost } from "@hookdeck/outpost-sdk";

const apiKey = process.env.OUTPOST_API_KEY;
if (!apiKey) throw new Error("Set OUTPOST_API_KEY");
const webhookUrl = process.env.OUTPOST_TEST_WEBHOOK_URL;
if (!webhookUrl) throw new Error("Set OUTPOST_TEST_WEBHOOK_URL");

const outpost = new Outpost({
  apiKey,
  ...(process.env.OUTPOST_API_BASE_URL
    ? { serverURL: process.env.OUTPOST_API_BASE_URL }
    : {}),
});

const tenantId = `ci_smoke_ts_${Math.random().toString(36).slice(2)}_${Date.now()}`;
const topic = process.env.OUTPOST_CI_PUBLISH_TOPIC ?? "user.created";

await outpost.tenants.upsert(tenantId);
await outpost.destinations.create(tenantId, {
  type: "webhook",
  topics: ["*"],
  config: { url: webhookUrl },
});
const published = await outpost.publish({
  tenantId,
  topic,
  eligibleForRetry: true,
  metadata: { source: "ci-smoke-ts" },
  data: { smoke: true },
});
console.log("smoke ts OK event id:", published.id);
EOTS

touch "$d01" "$d02"
echo "smoke-test-execute-ci: wrote $d01 and $d02 (publish topic=$PUBLISH_TOPIC)"
export OUTPOST_CI_PUBLISH_TOPIC="$PUBLISH_TOPIC"
./scripts/execute-ci-artifacts.sh
echo "smoke-test-execute-ci: OK"
