#!/bin/sh
set -e

echo "- Running: go run ./cmd/outpost-migrate-redis init --current"

go run ./cmd/outpost-migrate-redis init --current

echo "- Running: air serve"

exec air serve
