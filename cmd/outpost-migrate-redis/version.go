package main

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

const (
	schemaVersionKey = "outpost:schema:version"
	migrationLockKey = "outpost:migration:lock"
)

// getCurrentSchemaVersion returns the current schema version from Redis
// Returns 0 if no version is set (original schema)
func getCurrentSchemaVersion(ctx context.Context, client redis.Cmdable) (int, error) {
	versionStr, err := client.Get(ctx, schemaVersionKey).Result()
	if err == redis.Nil {
		// No version key means we're at version 0 (original schema)
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get schema version: %w", err)
	}

	version, err := strconv.Atoi(versionStr)
	if err != nil {
		return 0, fmt.Errorf("invalid schema version '%s': %w", versionStr, err)
	}

	return version, nil
}

// setSchemaVersion updates the schema version in Redis
func setSchemaVersion(ctx context.Context, client redis.Cmdable, version int) error {
	err := client.Set(ctx, schemaVersionKey, strconv.Itoa(version), 0).Err()
	if err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}
	return nil
}

// canRunMigration checks if a migration can run based on current version
// Migrations must run sequentially: v0→v1, v1→v2, etc.
func canRunMigration(currentVersion int, targetVersion int) bool {
	return currentVersion == targetVersion-1
}

// getNextMigration finds the next migration to run based on current version
func getNextMigration(currentVersion int) string {
	// Map of version to migration name
	// This could be more dynamic, but keeping it simple for now
	migrations := map[int]string{
		1: "001_hash_tags",
		// Future: 2: "002_add_ttl",
		// Future: 3: "003_compress_values",
	}

	if next, ok := migrations[currentVersion+1]; ok {
		return next
	}
	return ""
}

// MigrationLock represents a migration lock
type MigrationLock struct {
	Migration string    `json:"migration"`
	StartedAt time.Time `json:"started_at"`
	PID       int       `json:"pid"`
	Host      string    `json:"host"`
}

// acquireMigrationLock attempts to acquire a lock for running migrations
func acquireMigrationLock(ctx context.Context, client redis.Cmdable, migrationName string) error {
	// Check if lock exists
	exists, err := client.Exists(ctx, migrationLockKey).Result()
	if err != nil {
		return fmt.Errorf("failed to check lock: %w", err)
	}

	if exists == 1 {
		// Lock exists, check details
		lockData, err := client.Get(ctx, migrationLockKey).Result()
		if err != nil {
			return fmt.Errorf("failed to get lock details: %w", err)
		}

		return fmt.Errorf("migration is already running: %s\n"+
			"If this is a stale lock, run: migrateredis unlock", lockData)
	}

	// Create lock with details
	lock := fmt.Sprintf("migration=%s, started=%s", migrationName, time.Now().Format(time.RFC3339))

	// Set lock with 1 hour expiry (in case process dies without cleanup)
	err = client.SetEx(ctx, migrationLockKey, lock, time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	return nil
}

// releaseMigrationLock releases the migration lock
func releaseMigrationLock(ctx context.Context, client redis.Cmdable) error {
	err := client.Del(ctx, migrationLockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	return nil
}

// forceClearMigrationLock forcefully clears a migration lock (for stuck situations)
func forceClearMigrationLock(ctx context.Context, client redis.Cmdable) error {
	// Check if lock exists
	lockData, err := client.Get(ctx, migrationLockKey).Result()
	if err == redis.Nil {
		return fmt.Errorf("no migration lock found")
	}
	if err != nil {
		return fmt.Errorf("failed to get lock details: %w", err)
	}

	fmt.Printf("Current lock: %s\n", lockData)
	fmt.Printf("⚠️  WARNING: Clearing a lock while a migration is running could cause issues.\n")
	fmt.Printf("Only clear if you're certain the migration is not running.\n")
	fmt.Printf("Continue? (y/N): ")

	var response string
	fmt.Scanln(&response)
	if response != "y" && response != "Y" {
		fmt.Println("Lock clear cancelled.")
		return fmt.Errorf("lock clear cancelled")
	}

	err = client.Del(ctx, migrationLockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to clear lock: %w", err)
	}

	fmt.Println("✅ Migration lock cleared")
	return nil
}
