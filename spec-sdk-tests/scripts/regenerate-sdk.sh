#!/bin/bash
set -e

# Script lives at spec-sdk-tests/scripts/regenerate-sdk.sh; repo root is two levels up
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

# Default: run all SDKs. Optional: pass one or more of TS, Go, Python (comma-separated).
# Examples: ./regenerate-sdk.sh
#           ./regenerate-sdk.sh TS
#           ./regenerate-sdk.sh Go,Python
SDK_ARG="${1:-TS,Go,Python}"

# Normalize and validate: allow ts, go, python or TS, Go, Python
normalize() {
  echo "$1" | tr '[:upper:]' '[:lower:]'
}

run_ts() {
  echo "Regenerating TypeScript SDK..."
  speakeasy run -t outpost-ts
  echo "Rebuilding TypeScript SDK..."
  (cd "$REPO_ROOT/sdks/outpost-typescript" && npm run build)
  echo "TypeScript SDK regeneration and build complete."
}

run_go() {
  echo "Regenerating Go SDK..."
  speakeasy run -t outpost-go
  echo "Building Go SDK..."
  (cd "$REPO_ROOT/sdks/outpost-go" && go build ./...)
  echo "Go SDK regeneration and build complete."
}

run_python() {
  echo "Regenerating Python SDK..."
  speakeasy run -t outpost-python
  echo "Building Python SDK..."
  (cd "$REPO_ROOT/sdks/outpost-python" && pip install -e . -q)
  echo "Python SDK regeneration and build complete."
}

run_sdk() {
  local name
  name=$(normalize "$1")
  case "$name" in
    ts) run_ts ;;
    go) run_go ;;
    python) run_python ;;
    *)
      echo "Unknown SDK: $1. Use one or more of: TS, Go, Python (comma-separated)." >&2
      exit 1
      ;;
  esac
}

# Split comma-separated list (with optional spaces)
IFS=',' read -ra SDKS <<< "$SDK_ARG"
for s in "${SDKS[@]}"; do
  # Trim leading/trailing whitespace (bash-native)
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  [ -n "$s" ] || continue
  run_sdk "$s"
done

echo "All requested SDKs regenerated and built."
