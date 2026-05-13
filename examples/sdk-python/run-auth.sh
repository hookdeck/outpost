#!/usr/bin/env bash
# Run the auth example using a venv (avoids Poetry when lock/metadata issues occur).
# From repo root or examples/sdk-python: ./examples/sdk-python/run-auth.sh
# Or from examples/sdk-python: ./run-auth.sh
set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
SDK_DIR="$REPO_ROOT/sdks/outpost-python"

if [[ ! -d .venv ]]; then
  echo "Creating .venv..."
  python3 -m venv .venv
fi
.venv/bin/pip install -q -e "$SDK_DIR"
.venv/bin/pip install -q -r requirements.txt
echo "Running auth example..."
.venv/bin/python app.py auth
