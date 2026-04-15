#!/usr/bin/env bash
# Manual agent evaluation helper: prints paths and Turn 0 instructions.
# Does NOT invoke an LLM or run automated tests.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
REPO_ROOT="$(cd "$ROOT/../.." && pwd)"

usage() {
  echo "Usage: $0 <01|02|03|04|05|06|07|08|09|10>"
  echo "Prints the scenario file path and how to obtain Turn 0 from the single source of truth."
  echo ""
  echo "This script does not call an API or start an agent."
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" || -z "${1:-}" ]]; then
  usage
  exit 0
fi

id="$1"
shopt -s nullglob
matches=( "$ROOT/scenarios/${id}"-*.md )
shopt -u nullglob

if [[ ${#matches[@]} -eq 0 ]]; then
  echo "No scenario matching: scenarios/${id}-*.md" >&2
  exit 1
fi

scenario="${matches[0]}"

echo "=== Outpost agent eval (manual) ==="
echo ""
echo "Scenario file:"
echo "  $scenario"
echo ""
echo "Turn 0 — copy the fenced block under '## Template' from:"
echo "  $REPO_ROOT/docs/agent-evaluation/hookdeck-outpost-agent-prompt.md"
echo ""
echo "Placeholder examples (not the template):"
echo "  $ROOT/fixtures/placeholder-values-for-turn0.md"
echo ""
echo "Record results (local copy; see results/.gitignore):"
echo "  cp \"$ROOT/results/RUN-RECORDING.template.md\" \"$ROOT/results/$(date +%F)-s${id}-<client>.md\""
echo ""
