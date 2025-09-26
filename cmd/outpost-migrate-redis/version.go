package main

import (
	"context"
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
)

const (
	migrationLockKey = "outpost:migration:lock"
)

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
