#!/bin/bash
set -e

# Navigate to the TypeScript SDK directory
cd "$(dirname "$0")/../../../sdks/outpost-typescript"

# Regenerate the SDK using Speakeasy
echo "Regenerating TypeScript SDK..."
speakeasy run -t outpost-ts

# Rebuild the SDK
echo "Rebuilding TypeScript SDK..."
npm run build

echo "SDK regeneration and build complete."