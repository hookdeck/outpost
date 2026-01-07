// Package migrations provides a central registry for all Redis migrations.
// Both the CLI tool and app startup auto-migration use this registry.
// Add new migrations here - they will be available everywhere.
package migrations

import (
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"

	migration_001 "github.com/hookdeck/outpost/internal/migrator/migratorredis/001_hash_tags"
	migration_002 "github.com/hookdeck/outpost/internal/migrator/migratorredis/002_timestamps"
)

// MigrationFactory creates a migration instance with the given client and logger
type MigrationFactory func(client redis.Client, logger migratorredis.Logger) migratorredis.Migration

// registeredMigrations is the single source of truth for all migrations.
// Add new migrations here - they will be available to both CLI and auto-migration.
var registeredMigrations = []MigrationFactory{
	func(client redis.Client, logger migratorredis.Logger) migratorredis.Migration {
		return migration_001.New(client, logger)
	},
	func(client redis.Client, logger migratorredis.Logger) migratorredis.Migration {
		return migration_002.New(client, logger)
	},
}

// AllRedisMigrations returns all registered migrations instantiated with the given client and logger.
// This is the single registration point - add new migrations to registeredMigrations above.
func AllRedisMigrations(client redis.Client, logger migratorredis.Logger) []migratorredis.Migration {
	result := make([]migratorredis.Migration, len(registeredMigrations))
	for i, factory := range registeredMigrations {
		result[i] = factory(client, logger)
	}
	return result
}

// AllRedisMigrationsWithLogging returns all migrations using logging.Logger (convenience for app startup)
func AllRedisMigrationsWithLogging(client redis.Client, logger *logging.Logger, verbose bool) []migratorredis.Migration {
	adapter := migratorredis.NewLoggerAdapter(logger, verbose)
	return AllRedisMigrations(client, adapter)
}
