# Test Cases: #680 Control Plane Key Scoping

## Setup

Uses the standard dev environment (see [contributing/getting-started.md](../../contributing/getting-started.md)).

```bash
# Start deps (Redis + RabbitMQ + Postgres)
make up/deps
```

For tests that need old vs new binaries:

```bash
# Build from main (before fix)
git checkout main
go build -o bin/outpost-old ./cmd/outpost

# Build from feature branch
git checkout <branch>
go build -o bin/outpost-new ./cmd/outpost
go build -o bin/outpost-migrate-redis-new ./cmd/outpost-migrate-redis
```

Redis verification commands below are raw Redis commands — run them from `redis-cli` or any Redis client connected to `localhost:26379`.

---

## TC1: Reproduce bug — two deployments share control plane keys

```
FLUSHALL
```

Start two deployments using Docker Compose env overrides (or run manually):

```bash
# Terminal 1 — deployment A
DEPLOYMENT_ID=dp_A \
REDIS_HOST=host.docker.internal REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3333 \
  go run ./cmd/outpost --config .outpost.yaml

# Terminal 2 — deployment B
DEPLOYMENT_ID=dp_B \
REDIS_HOST=host.docker.internal REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3334 \
  go run ./cmd/outpost --config .outpost.yaml
```

> Note: If running outside Docker, use `localhost` for REDIS_HOST. The `.outpost.yaml` redis config is overridden by env vars.

Verify shared keys (the bug):

```
KEYS *outpost*
# outpostrc                           ← shared, NOT dp_A: or dp_B: prefixed
# outpost:migration:001_hash_tags     ← shared
# outpost:migration:002_timestamps    ← shared
# outpost:migration:003_entity        ← shared

HGETALL outpostrc
# installation → inst_xxx  ← one value shared by both deployments
```

Stop both.

---

## TC2: New version — scoped keys on fresh Redis

```
FLUSHALL
```

Start two deployments with the new binary:

```bash
# Terminal 1
DEPLOYMENT_ID=dp_A \
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3333 \
  ./bin/outpost-new

# Terminal 2
DEPLOYMENT_ID=dp_B \
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3334 \
  ./bin/outpost-new
```

Verify:

```
KEYS *outpost*
# dp_A:outpost:installation_id
# dp_A:outpost:migration:001_hash_tags
# dp_A:outpost:migration:002_timestamps
# dp_A:outpost:migration:003_entity
# dp_B:outpost:installation_id
# dp_B:outpost:migration:001_hash_tags
# dp_B:outpost:migration:002_timestamps
# dp_B:outpost:migration:003_entity

# Installation IDs are different
GET dp_A:outpost:installation_id   # → inst_aaa
GET dp_B:outpost:installation_id   # → inst_bbb (different)

# Migration status is per-deployment
HGETALL dp_A:outpost:migration:001_hash_tags
# status → not_applicable (or applied)

# No unscoped keys
EXISTS outpostrc                   # → 0
KEYS outpost:migration:*           # → (empty)
```

Stop both.

---

## TC3: No DEPLOYMENT_ID (single-tenant) still works

```
FLUSHALL
```

Start without DEPLOYMENT_ID (default dev flow):

```bash
make up/outpost
# or:
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3333 \
  ./bin/outpost-new
```

Verify:

```
KEYS *outpost*
# outpost:installation_id              ← no prefix, new key name
# outpost:migration:001_hash_tags      ← no prefix
# outpost:migration:002_timestamps
# outpost:migration:003_entity

GET outpost:installation_id
# → inst_xxx (not empty)

HGET outpost:migration:001_hash_tags status
# → applied or not_applicable
```

---

## TC4: Migration CLI respects scoping

```
FLUSHALL
```

```bash
# Init with deployment ID
DEPLOYMENT_ID=dp_A REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
  ./bin/outpost-migrate-redis-new init
```

```
KEYS *
# dp_A:outpost:migration:001_hash_tags
# dp_A:outpost:migration:002_timestamps
# dp_A:outpost:migration:003_entity
```

```bash
# Init without — separate namespace
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
  ./bin/outpost-migrate-redis-new init
```

```
KEYS *
# dp_A:outpost:migration:*   ← dp_A's keys
# outpost:migration:*        ← unscoped keys (separate)
```

---

## TC5: Upgrade path — old data → migrate script → new version

```
FLUSHALL
```

```bash
# 1. Run old version, let it write shared keys
DEPLOYMENT_ID=dp_A \
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3333 \
  ./bin/outpost-old

# Create a tenant so there's data
curl -X PUT http://localhost:3333/api/v1/tenants/test-tenant \
  -H "Authorization: Bearer apikey"
# Stop the server
```

```
# 2. Verify old shared keys exist
KEYS *
# outpostrc, outpost:migration:*, dp_A:tenant:{test-tenant}:tenant, etc.
```

```bash
# 3. Run the migration script (dry run, then apply)
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
  ./scripts/issue-680/migrate.sh

REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
  ./scripts/issue-680/migrate.sh --apply
```

```
# 4. Verify scoped keys were created
KEYS dp_A:outpost:*
# dp_A:outpost:installation_id
# dp_A:outpost:migration:001_hash_tags
# ...
```

```bash
# 5. Start new version
DEPLOYMENT_ID=dp_A \
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3333 \
  ./bin/outpost-new
# Should start cleanly — no migration errors, no re-initialization

# 6. Verify tenant data still accessible
curl http://localhost:3333/api/v1/tenants \
  -H "Authorization: Bearer apikey"
# → should list test-tenant
```

---

## TC6: Concurrent startup — same deployment, two services

Tests atomic installation ID generation under the new key scheme.

```
FLUSHALL
```

```bash
# Start API and delivery for same deployment simultaneously
DEPLOYMENT_ID=dp_A \
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=api PORT=3333 \
  ./bin/outpost-new &

DEPLOYMENT_ID=dp_A \
REDIS_HOST=localhost REDIS_PORT=26379 REDIS_PASSWORD=password \
SERVICE=delivery \
  ./bin/outpost-new &

wait
```

```
# Both should share the same installation ID (SetNX ensures atomicity)
GET dp_A:outpost:installation_id
# → exactly one value
```
