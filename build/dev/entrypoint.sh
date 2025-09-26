#!/bin/sh
set -e

go run ./cmd/outpost-migrate-redis init --current

exec air serve
