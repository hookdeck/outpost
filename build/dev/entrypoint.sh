#!/bin/sh
set -e

# The outpost server refuses to start if any SQL or Redis migrations are
# pending (see internal/app/migration.go). If that happens during dev,
# run 'make migrate' from the host to apply pending migrations.

echo "- Running: air serve"

exec air serve
