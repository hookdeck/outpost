# Redis Migration Tool

A schema migration tool for Redis that manages versioned migrations with state tracking directly in Redis.

## Purpose

This tool manages Redis schema changes in a controlled, versioned manner. It tracks state using Redis keys:
- `outpost:schema:version` - Current schema version number
- `outpost:migration:<name>:state` - State of each migration (persisted, no TTL)
- `outpost:migration:lock` - Prevents concurrent migrations (auto-expires after 1 hour)

**Note**: Currently designed for manual migrations with downtime. Not yet suitable for zero-downtime migrations, but provides a foundation for future enhancements.

## Usage

```bash
# Check current status
./migrateredis status

# List available migrations
./migrateredis list

# Run a specific migration (with optional flags)
./migrateredis -migration 001_hash_tags plan    # Dry run - shows what will change
./migrateredis -migration 001_hash_tags apply    # Execute the migration
./migrateredis -migration 001_hash_tags verify   # Validate migration succeeded
./migrateredis -migration 001_hash_tags cleanup  # Remove old keys after verification

# If a migration gets stuck
./migrateredis unlock  # Force clear the migration lock (use with caution)
```

## Migration Workflow

1. **Plan** - Analyze current state and show what will be migrated
2. **Apply** - Execute the migration (creates new keys, preserves old ones)
3. **Verify** - Spot-check migrated data for correctness
4. **Cleanup** - Delete old keys after confirming success

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

Reads Redis connection from environment variables (supports `.env` file):
- `REDIS_HOST` - Redis server hostname
- `REDIS_PORT` - Redis server port
- `REDIS_PASSWORD` - Redis password for authentication
- `REDIS_DATABASE` - Redis database number (ignored in cluster mode)
- `REDIS_TLS_ENABLED` - Enable TLS encryption for Redis connection (true/false)
- `REDIS_CLUSTER_ENABLED` - Enable Redis cluster mode (true/false)