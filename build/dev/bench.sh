#!/usr/bin/env bash
# Runs one benchmark end to end: spec → run → Prometheus → export → figures.
#
# Nothing here is interactive and nothing needs Grafana. Grafana is for
# watching a run in progress; the artifact comes out of this script.
#
# The spec is client-side input: it is read here and POSTed to the app, so the
# deployment never needs a copy. Everything the script touches on the app goes
# over HTTP, which is what lets the same command drive a local stack or a
# deployed one:
#
#   bench.sh                            # the example spec against localhost
#   bench.sh path/to/spec.yaml
#   LOADTEST_URL=https://… PROM_URL=https://… bench.sh path/to/spec.yaml
#   bench.sh --detach path/to/spec.yaml # start it and exit; render later
#   bench.sh --report <run_id>          # render a run that already finished
#
# --detach exists because a long run should not need the operator's laptop
# awake for its whole window. Start it, walk away, report on it afterwards.
set -euo pipefail

APP="${LOADTEST_URL:-http://localhost:9090}"
PROM="${PROM_URL:-http://localhost:9091}"
OUT="${BENCH_OUT:-loadtest/out}"
REPORT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)/loadtest/report"

DETACH=0
REPORT_ONLY=""
SPEC=""
while [ $# -gt 0 ]; do
  case "$1" in
    --detach) DETACH=1; shift ;;
    --report) REPORT_ONLY="${2:-}"; shift 2 ;;
    -*) echo "error: unknown flag $1" >&2; exit 2 ;;
    *) SPEC="$1"; shift ;;
  esac
done
SPEC="${SPEC:-loadtest/app/runs/example.yaml}"

need() { command -v "$1" >/dev/null || { echo "error: $1 is required" >&2; exit 2; }; }
need curl
need jq
need python3

# Auth for a deployed app. Local dev has none, so the header is omitted when
# the variable is unset rather than sent empty.
AUTH=()
if [ -n "${LOADTEST_TOKEN:-}" ]; then
  AUTH=(-H "Authorization: Bearer $LOADTEST_TOKEN")
fi

# --- report: fetch the artifact over HTTP, enrich it, render it -------------
# Fetching rather than reading the filesystem is what makes a remote target
# work: the app writes the export into its own container, which the operator's
# machine cannot see.
report() {
  local run_id="$1"
  # Retrying only makes sense straight after a run, when the export may still
  # be being written. Asked for a run that finished long ago — or a typo — the
  # first 404 is the answer, and waiting half a minute for it is just noise.
  local attempts="${2:-1}"
  mkdir -p "$OUT"
  local artifact="$OUT/$run_id.json"

  local ok=0
  for _ in $(seq 1 "$attempts"); do
    if curl -fsS "${AUTH[@]}" "$APP/api/runs/$run_id" -o "$artifact.tmp" 2>/dev/null \
       && jq -e '.run.id' "$artifact.tmp" >/dev/null 2>&1; then
      mv "$artifact.tmp" "$artifact"; ok=1; break
    fi
    sleep 2
  done
  rm -f "$artifact.tmp"
  if [ "$ok" != "1" ]; then
    echo "error: no export available for $run_id at $APP" >&2
    exit 1
  fi

  local voids
  voids="$(jq -r '.run.voids // [] | length' "$artifact")"
  if [ "$voids" != "0" ]; then
    echo "==> run is VOID:"
    jq -r '.run.voids[] | "    - \(.)"' "$artifact"
  fi

  echo "==> fetching series from prometheus"
  python3 "$REPORT_DIR/fetch.py" --prom "$PROM" --artifact "$artifact" --out "$artifact"

  # Raw capture is separate from the enriched export and much larger: full
  # native-histogram buckets at the scrape interval, over the whole run rather
  # than the steady window. It is what makes a question that occurs to someone
  # next month answerable without paying for the run again.
  echo "==> archiving raw series"
  python3 "$REPORT_DIR/capture.py" --prom "$PROM" --artifact "$artifact" \
    --out "${artifact%.json}" || echo "warning: raw capture failed" >&2

  echo "==> rendering figures"
  python3 "$REPORT_DIR/make_charts.py" --data "$artifact"

  jq -r '"==> " + .run.id,
         "    published \(.total.published)  completed \(.total.completed)  missing \(.total.missing)  cutoff \(.total.cutoff)",
         "    export    '"$artifact"'"' "$artifact"
  echo "    figures   $REPORT_DIR/charts/"
  [ "$voids" != "0" ] && echo "    VOID — figures carry the void notice; do not publish" && exit 3
  exit 0
}

if [ -n "$REPORT_ONLY" ]; then
  report "$REPORT_ONLY"
fi

if [ ! -f "$SPEC" ]; then
  echo "error: spec not found: $SPEC" >&2
  exit 2
fi

# --- preflight -------------------------------------------------------------
if ! curl -fsS "${AUTH[@]}" "$APP/api/status" >/dev/null 2>&1; then
  echo "error: loadtest app not reachable at $APP — run 'make up/bench' first" >&2
  exit 1
fi
if ! curl -fsS "$PROM/-/ready" >/dev/null 2>&1; then
  echo "error: prometheus not reachable at $PROM — run 'make up/bench' first" >&2
  exit 1
fi

# Fail before provisioning rather than after: a spec that busts its budget
# measures saturation, and finding that out an hour in is expensive.
echo "==> validating $SPEC"
validation="$(curl -sS "${AUTH[@]}" -X POST "$APP/api/runs/validate" --data-binary @"$SPEC")"
if [ "$(jq -r '.valid' <<<"$validation")" != "true" ]; then
  jq -r '.error' <<<"$validation" >&2
  exit 1
fi
jq -r '.budget | "    \(.offered_rate) events/s · \(.concurrency|floor) concurrent deliveries · \(.bytes_per_sec/1000000*100|floor/100) MB/s"' <<<"$validation"

# --- run -------------------------------------------------------------------
echo "==> starting run"
started="$(curl -sS "${AUTH[@]}" -X POST "$APP/api/runs" --data-binary @"$SPEC")"
RUN_ID="$(jq -r '.id' <<<"$started")"
if [ "$RUN_ID" = "null" ] || [ -z "$RUN_ID" ]; then
  echo "$started" >&2
  exit 1
fi
echo "    run_id=$RUN_ID"

if [ "$DETACH" = "1" ]; then
  cat <<EOF
    detached — the run continues on the app.
    watch:   curl -s $APP/api/runs/current | jq -r .phase
    report:  $0 --report $RUN_ID
EOF
  exit 0
fi

# Abort the run if the operator interrupts, so a killed script doesn't leave
# publishers hammering the deployment.
trap 'echo; echo "==> aborting run"; curl -sS "${AUTH[@]}" -X POST "$APP/api/runs/current/abort" >/dev/null || true; exit 130' INT TERM

phase=""
while true; do
  cur="$(curl -sS "${AUTH[@]}" "$APP/api/runs/current")"
  p="$(jq -r '.phase' <<<"$cur")"
  if [ "$p" != "$phase" ]; then
    printf '    %-9s %s\n' "$p" "$(date +%H:%M:%S)"
    phase="$p"
  fi
  case "$p" in
    complete) break ;;
    failed|aborted)
      jq -r '.error // "run did not complete"' <<<"$cur" >&2
      exit 1
      ;;
  esac
  sleep 5
done

report "$RUN_ID" 15
