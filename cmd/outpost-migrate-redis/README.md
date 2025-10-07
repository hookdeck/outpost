# Outpost Migration Tool

A CLI tool for managing database schema migrations for Outpost, with support for versioned migrations and state tracking.

## Purpose

This tool manages database schema changes in a controlled manner. It tracks state using Redis keys:
- `outpost:migration:<name>` - Hash storing migration state (fields: status="applied", applied_at=timestamp)
- `outpost:migration:lock` - Prevents concurrent migrations (auto-expires after 1 hour)

**Note**: Currently designed for manual migrations with downtime. Not yet suitable for zero-downtime migrations, but provides a foundation for future enhancements.

## Installation

### Using the Outpost CLI wrapper
```bash
# Build all binaries
make build

# Run via the wrapper
./bin/outpost migrate [command]
```

### Direct execution
```bash
# Build just this tool
go build -o bin/outpost-migrate-redis ./cmd/outpost-migrate-redis

# Run directly
./bin/outpost-migrate-redis [command]
```

### Development
```bash
# Run without building (via wrapper)
go run ./cmd/outpost migrate [command]

# Run directly
go run ./cmd/outpost-migrate-redis [command]
```

## Usage

```bash
# Initialize database for fresh installations (runs on startup)
outpost migrate init
outpost migrate init --current  # Exit 1 if migrations pending (for CI/CD)

# List available migrations
outpost migrate list

# Plan next migration (shows current status and what will change)
outpost migrate plan

# Apply the migration (creates new keys, preserves old ones)
outpost migrate apply
outpost migrate apply --yes  # Skip confirmation prompt

# Verify the migration was successful
outpost migrate verify

# Cleanup old keys after verification
outpost migrate cleanup
outpost migrate cleanup --yes  # Skip confirmation
outpost migrate cleanup --force  # Skip verification check

# Force clear the migration lock (use with caution)
outpost migrate unlock
outpost migrate unlock --yes  # Skip confirmation prompt
```

## Migration Workflow

1. **Plan** - Check status and show what will be migrated
2. **Apply** - Execute the migration (creates new keys, preserves old ones)
3. **Verify** - Spot-check migrated data for correctness
4. **Cleanup** - Delete old keys after confirming success

## Using in Startup Scripts

The `init --current` command is designed for use in automated startup scripts. It handles both fresh installations and existing deployments:

```bash
# Initialize database and check for pending migrations
outpost migrate init --current || {
    echo "Error: Database migrations required"
    echo "Run: outpost migrate apply"
    exit 1
}
outpost serve
```

### Init Command Behavior

The `init` command intelligently handles different scenarios:

- **Fresh Installation**: Automatically marks all migrations as applied without running them (since the schema is already current)
- **Existing Installation**: Checks if migrations are pending
- **With `--current` flag**: Exits with code 1 if migrations are pending, making it perfect for CI/CD pipelines
- **Multi-node deployments**: Uses atomic locking to ensure only one node performs initialization

This eliminates the need to manually handle fresh installations differently from existing ones.

## Available Migrations

### 001_hash_tags
Migrates Redis keys from legacy format to hash-tagged format for Redis Cluster compatibility.

**Purpose:** Ensures all keys for a tenant are routed to the same Redis Cluster node by using hash tags.

**Key Transformations:**
- `tenant:123` → `tenant:{123}:tenant`
- `tenant:123:destinations` → `tenant:{123}:destinations`
- `tenant:123:destination:abc` → `tenant:{123}:destination:abc`

**Deployment Mode Note:** If you are using `DEPLOYMENT_ID` configuration, this migration is **not needed**. Deployment-scoped keys already include hash tags:
- `dp_001:tenant:{123}:tenant` (already has hash tags)
- `dp_001:tenant:{123}:destinations` (already has hash tags)

See [001_hash_tags/README.md](./migration/001_hash_tags/README.md) for details.

**Safety:** This migration preserves original keys. Use the cleanup command after verification to remove old keys.

## Adding New Migrations

1. Create a new directory: `migration/002_your_migration_name/`
2. Implement the `Migration` interface:
   ```go
   type Migration interface {
       Name() string          // e.g., "002_your_migration"
       Version() int          // Target schema version (2)
       Description() string
       Plan(ctx, client, verbose) (*Plan, error)
       Apply(ctx, client, plan, verbose) (*State, error)
       Verify(ctx, client, state, verbose) (*VerificationResult, error)
       Cleanup(ctx, client, state, force, verbose) error
   }
   ```
3. Register it in `main.go`:
   ```go
   registry.Register(migration_002_your_migration.New())
   ```

## Lock Mechanism

The tool uses a simple Redis lock (`outpost:migration:lock`) to prevent concurrent migrations. The lock auto-expires after 1 hour as a safety measure. Use `unlock` command only when certain no migration is running.

## Configuration

The tool uses Outpost's standard configuration system, loading settings from (in order of precedence):

1. **CLI flags** (highest priority)
2. **Environment variables**
3. **Config files** (`.outpost.yaml`, `.env`)
4. **Default values**

### Redis Configuration Options

| Option | CLI Flag | Env Variable | Description | Default |
|--------|----------|--------------|-------------|---------|
| Host | `--redis-host` | `REDIS_HOST` | Redis server hostname | `redis` |
| Port | `--redis-port` | `REDIS_PORT` | Redis server port | `6379` |
| Password | `--redis-password` | `REDIS_PASSWORD` | Redis password | (empty) |
| Database | `--redis-database` | `REDIS_DATABASE` | Database number (0-15) | `0` |
| Cluster Mode | `--redis-cluster` | `REDIS_CLUSTER_ENABLED` | Enable cluster mode | `false` |
| TLS | `--redis-tls` | `REDIS_TLS_ENABLED` | Enable TLS connection | `false` |

### Other Options

| Option | CLI Flag | Description |
|--------|----------|-------------|
| Config File | `--config, -c` | Path to config file |
| Verbose | `--verbose` | Enable verbose output (shows Redis config) |

### Examples

```bash
# Using environment variables
REDIS_HOST=localhost outpost migrate plan

# Using CLI flags
outpost migrate --redis-host localhost --verbose plan

# Using config file
outpost migrate --config /path/to/config.yaml plan

# Production cluster with TLS
outpost migrate \
  --redis-host redis-cluster.example.com \
  --redis-cluster \
  --redis-tls \
  --verbose \
  plan
```