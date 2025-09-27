package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration"
	migration_001 "github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration/001_hash_tags"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/redis"
)

const (
	migrationLockKey = ".outpost:migration:lock"
)

// Global migration registry
var registry *migration.Registry

func init() {
	// Initialize and register all migrations
	registry = migration.NewRegistry()

	// Register migrations here
	registry.Register(migration_001.New())

	// Future migrations would be registered like:
	// registry.Register(migration_002.New())
	// registry.Register(migration_003.New())
}

// Migrator handles Redis migrations
type Migrator struct {
	client  *redisClientWrapper
	verbose bool
}

// NewMigrator creates a new migrator instance
func NewMigrator(cfg *config.Config, verbose bool) (*Migrator, error) {
	ctx := context.Background()

	// Build Redis client
	redisConfig := cfg.Redis.ToConfig()
	redisClient, err := redis.New(ctx, redisConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Create wrapper client
	client := &redisClientWrapper{
		Cmdable: redisClient,
	}

	return &Migrator{
		client:  client,
		verbose: verbose,
	}, nil
}

// ListMigrations lists all available migrations
func (m *Migrator) ListMigrations() error {
	migrations := registry.GetAll()
	if len(migrations) == 0 {
		fmt.Println("No migrations registered")
		return nil
	}

	// Sort migration names for consistent output
	var names []string
	for name := range migrations {
		names = append(names, name)
	}
	sort.Strings(names)

	fmt.Println("Available migrations:")
	for _, name := range names {
		mig := migrations[name]
		fmt.Printf("  %s - %s\n", name, mig.Description())
	}
	return nil
}

// acquireLock attempts to acquire a lock for running migrations
func (m *Migrator) acquireLock(ctx context.Context, migrationName string) error {
	// Create lock with details
	lock := fmt.Sprintf("migration=%s, started=%s", migrationName, time.Now().Format(time.RFC3339))

	// Try to set lock atomically with SetNX (only sets if not exists)
	// Use 1 hour expiry in case process dies without cleanup
	success, err := m.client.SetNX(ctx, migrationLockKey, lock, time.Hour).Result()
	if err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	if !success {
		// Lock already exists, get details for error message
		lockData, err := m.client.Get(ctx, migrationLockKey).Result()
		if err != nil {
			return fmt.Errorf("migration is already running (could not get lock details: %w)", err)
		}

		return fmt.Errorf("migration is already running: %s\n"+
			"If this is a stale lock, run: migrateredis unlock", lockData)
	}

	return nil
}

// releaseLock releases the migration lock
func (m *Migrator) releaseLock(ctx context.Context) error {
	err := m.client.Del(ctx, migrationLockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to release lock: %w", err)
	}
	return nil
}

// Unlock forcefully clears the migration lock (for stuck situations)
func (m *Migrator) Unlock(ctx context.Context, autoApprove bool) error {
	// Check if lock exists
	lockData, err := m.client.Get(ctx, migrationLockKey).Result()
	if err != nil && err.Error() == "redis: nil" {
		fmt.Println("No migration lock found")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to get lock details: %w", err)
	}

	fmt.Printf("Current lock: %s\n", lockData)

	if !autoApprove {
		fmt.Printf("⚠️  WARNING: Clearing a lock while a migration is running could cause issues.\n")
		fmt.Printf("Only clear if you're certain the migration is not running.\n")
		fmt.Printf("Continue? (y/N): ")

		var response string
		fmt.Scanln(&response)
		if response != "y" && response != "Y" {
			fmt.Println("Lock clear cancelled.")
			return nil
		}
	}

	err = m.client.Del(ctx, migrationLockKey).Err()
	if err != nil {
		return fmt.Errorf("failed to clear lock: %w", err)
	}

	fmt.Println("✅ Migration lock cleared")
	return nil
}

// Init handles initialization for fresh installations
func (m *Migrator) Init(ctx context.Context, currentCheck bool) error {
	fmt.Println("Checking Redis installation status...")

	// Check if Redis has any existing data first
	isFresh, err := m.checkIfFreshInstallation(ctx)
	if err != nil {
		return fmt.Errorf("failed to check installation status: %w", err)
	}

	if isFresh {
		fmt.Println("Fresh installation detected, acquiring lock...")

		// Try to acquire lock - this is atomic with SetNX
		err := m.acquireLock(ctx, "init")
		if err != nil {
			// Someone else is initializing - wait for them
			fmt.Println("Another process is initializing, waiting...")

			// Wait for initialization to complete (check every second, max 30 seconds)
			for i := 0; i < 30; i++ {
				time.Sleep(1 * time.Second)

				stillFresh, err := m.checkIfFreshInstallation(ctx)
				if err != nil {
					return fmt.Errorf("failed to check installation status: %w", err)
				}

				if !stillFresh {
					fmt.Println("✅ Redis initialized by another process")
					return nil
				}
			}

			return fmt.Errorf("timeout waiting for Redis initialization")
		}

		// We got the lock
		fmt.Println("Lock acquired successfully")
		defer m.releaseLock(ctx)

		isFresh, err = m.checkIfFreshInstallation(ctx)
		if err != nil {
			return fmt.Errorf("failed to recheck installation status: %w", err)
		}

		if !isFresh {
			// Someone else initialized while we were acquiring lock
			fmt.Println("✅ Redis initialized by another process")
			return nil
		}

		// Still fresh and we have the lock - do the initialization
		fmt.Println("Initializing Redis...")

		// Mark all migrations as applied
		migrations := registry.GetAll()
		for name := range migrations {
			if err := setMigrationAsApplied(ctx, m.client, name); err != nil {
				return fmt.Errorf("failed to mark migration %s as applied: %w", name, err)
			}
		}

		fmt.Printf("✅ Redis initialized successfully - marked %d migration(s) as applied\n", len(migrations))
		return nil
	}

	// Not fresh - Redis already initialized (no lock needed)
	fmt.Println("Redis already initialized")

	// If --current flag is set, check if migrations are pending
	if currentCheck {
		// Get pending migrations count
		pendingCount := 0
		migrations := registry.GetAll()
		for _, mig := range migrations {
			if !isApplied(ctx, m.client, mig.Name()) {
				pendingCount++
			}
		}

		if pendingCount > 0 {
			fmt.Fprintf(os.Stderr, "Migration required: %d pending\n", pendingCount)
			os.Exit(1)
		}
		// Up to date - exit normally
	}

	return nil
}

// checkIfFreshInstallation checks if Redis is a fresh installation
func (m *Migrator) checkIfFreshInstallation(ctx context.Context) (bool, error) {
	// Check for any "outpost:*" keys (current format)
	outpostKeys, err := m.client.Keys(ctx, "outpost:*").Result()
	if err != nil {
		return false, fmt.Errorf("failed to check outpost keys: %w", err)
	}
	if len(outpostKeys) > 0 {
		return false, nil // Has current data
	}

	// Check for any "tenant:*" keys (old format)
	tenantKeys, err := m.client.Keys(ctx, "tenant:*").Result()
	if err != nil {
		return false, fmt.Errorf("failed to check tenant keys: %w", err)
	}
	if len(tenantKeys) > 0 {
		return false, nil // Has old data
	}

	// No keys found - it's a fresh installation
	return true, nil
}

// Plan shows what changes would be made without applying them
func (m *Migrator) Plan(ctx context.Context) error {
	// First show current status
	migrations := registry.GetAll()
	var appliedCount, pendingCount int
	var nextMigration migration.Migration

	for _, mig := range migrations {
		if isApplied(ctx, m.client, mig.Name()) {
			appliedCount++
		} else {
			pendingCount++
			if nextMigration == nil {
				nextMigration = mig
			}
		}
	}

	fmt.Println("Migration Status:")
	fmt.Printf("  Applied: %d migration(s)\n", appliedCount)
	fmt.Printf("  Pending: %d migration(s)\n", pendingCount)

	if pendingCount == 0 {
		fmt.Println("\nAll migrations have been applied. Nothing to plan.")
		return nil
	}

	// Get the next unapplied migration
	mig, err := getNextMigration(ctx, m.client)
	if err != nil {
		return err
	}

	// Run the migration in plan mode
	plan, err := mig.Plan(ctx, m.client, m.verbose)
	if err != nil {
		return err
	}

	// Display the plan
	fmt.Printf("\nNext Migration: %s\n", mig.Name())
	fmt.Printf("  Description: %s\n", plan.Description)
	fmt.Printf("  Estimated items: %d\n", plan.EstimatedItems)
	if len(plan.Scope) > 0 {
		fmt.Println("  Scope:")
		for key, value := range plan.Scope {
			fmt.Printf("    %s: %d\n", key, value)
		}
	}

	return nil
}

// Verify verifies that a migration was successful
func (m *Migrator) Verify(ctx context.Context) error {
	// Get the last applied migration
	mig, err := getLastAppliedMigration(ctx, m.client)
	if err != nil {
		return err
	}

	fmt.Printf("Verifying migration %s...\n", mig.Name())

	// Run verification
	// Note: We're passing a minimal state object since the full state isn't stored yet
	verifyState := &migration.State{
		MigrationName: mig.Name(),
		Phase:         "applied",
	}

	result, err := mig.Verify(ctx, m.client, verifyState, m.verbose)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	// Display results
	if result.Valid {
		fmt.Println("✅ Migration verified successfully")
		fmt.Printf("  Checks run: %d\n", result.ChecksRun)
		fmt.Printf("  Checks passed: %d\n", result.ChecksPassed)
	} else {
		fmt.Println("❌ Migration verification failed")
		fmt.Printf("  Checks run: %d\n", result.ChecksRun)
		fmt.Printf("  Checks passed: %d\n", result.ChecksPassed)
		if len(result.Issues) > 0 {
			fmt.Println("  Issues found:")
			for _, issue := range result.Issues {
				fmt.Printf("    - %s\n", issue)
			}
		}
		return fmt.Errorf("migration verification failed")
	}

	if m.verbose && len(result.Details) > 0 {
		fmt.Println("  Details:")
		for key, value := range result.Details {
			fmt.Printf("    %s: %s\n", key, value)
		}
	}

	return nil
}

// Cleanup removes old keys after successful migration
func (m *Migrator) Cleanup(ctx context.Context, force, autoApprove bool) error {
	// Get the last applied migration
	mig, err := getLastAppliedMigration(ctx, m.client)
	if err != nil {
		return err
	}

	// First verify the migration if not forced
	if !force {
		fmt.Println("Verifying migration before cleanup...")
		verifyState := &migration.State{
			MigrationName: mig.Name(),
			Phase:         "applied",
		}

		result, err := mig.Verify(ctx, m.client, verifyState, m.verbose)
		if err != nil {
			return fmt.Errorf("verification failed: %w", err)
		}

		if !result.Valid {
			return fmt.Errorf("migration verification failed - cleanup aborted. Use --force to override")
		}
		fmt.Println("✅ Verification passed")
	}

	fmt.Printf("Analyzing cleanup for migration %s...\n", mig.Name())

	// Get a preview of what will be cleaned up by running a dry-run plan
	// This helps us show what will be deleted before confirming
	plan, err := mig.Plan(ctx, m.client, false)
	if err != nil {
		return fmt.Errorf("failed to analyze cleanup scope: %w", err)
	}

	// Estimate cleanup impact from the plan
	if plan.EstimatedItems == 0 {
		fmt.Println("No old keys to cleanup.")
		return nil
	}

	// Confirm if not auto-approved
	if !autoApprove && !force {
		fmt.Printf("\n⚠️  WARNING: This will delete approximately %d old Redis keys.\n", plan.EstimatedItems)
		fmt.Println("This action cannot be undone.")
		fmt.Print("\nDo you want to continue? (yes/no): ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			fmt.Println("Cleanup cancelled.")
			return nil
		}
	}

	// Acquire lock before cleanup
	fmt.Printf("\nAcquiring lock for cleanup...\n")
	if err := m.acquireLock(ctx, fmt.Sprintf("cleanup-%s", mig.Name())); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	fmt.Println("Lock acquired successfully")
	defer m.releaseLock(ctx)

	fmt.Printf("Cleaning up old keys from migration %s...\n", mig.Name())

	// Run cleanup
	// Note: We're passing a minimal state object since the full state isn't stored yet
	cleanupState := &migration.State{
		MigrationName: mig.Name(),
		Phase:         "applied",
	}

	// Run cleanup (migration should not handle confirmations)
	err = mig.Cleanup(ctx, m.client, cleanupState, m.verbose)
	if err != nil {
		return fmt.Errorf("cleanup failed: %w", err)
	}

	fmt.Println("✅ Cleanup completed successfully")
	fmt.Println("Old keys have been removed.")

	return nil
}

// Apply executes the migration
func (m *Migrator) Apply(ctx context.Context, autoApprove bool) error {
	// Get the next unapplied migration
	mig, err := getNextMigration(ctx, m.client)
	if err != nil {
		if err.Error() == "all migrations have been applied" {
			fmt.Println("All migrations have been applied. Nothing to do.")
			return nil
		}
		return err
	}

	// First show the plan
	fmt.Println("Planning migration...")
	plan, err := mig.Plan(ctx, m.client, m.verbose)
	if err != nil {
		return err
	}

	fmt.Printf("  Description: %s\n", plan.Description)
	fmt.Printf("  Estimated items: %d\n", plan.EstimatedItems)

	// Confirm if not auto-approved
	if !autoApprove {
		fmt.Print("\nDo you want to apply these changes? (yes/no): ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			fmt.Println("Migration cancelled.")
			return nil
		}
	}

	// Acquire lock before applying migration
	fmt.Println("\nAcquiring migration lock...")
	if err := m.acquireLock(ctx, mig.Name()); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}
	fmt.Println("Lock acquired successfully")
	defer m.releaseLock(ctx)

	// Apply the migration
	fmt.Println("Applying migration...")
	state, err := mig.Apply(ctx, m.client, plan, m.verbose)
	if err != nil {
		return err
	}

	// Mark migration as applied
	if err := setMigrationAsApplied(ctx, m.client, mig.Name()); err != nil {
		return fmt.Errorf("failed to mark migration as applied: %w", err)
	}

	fmt.Printf("Migration completed successfully.\n")
	fmt.Printf("  Processed items: %d\n", state.Progress.ProcessedItems)
	fmt.Printf("  Failed items: %d\n", state.Progress.FailedItems)
	fmt.Printf("  Skipped items: %d\n", state.Progress.SkippedItems)
	return nil
}

// isApplied checks if a migration has been applied
func isApplied(ctx context.Context, client *redisClientWrapper, name string) bool {
	key := fmt.Sprintf("outpost:migration:%s", name)
	val, err := client.HGet(ctx, key, "status").Result()
	if err != nil {
		return false
	}
	return val == "applied"
}

// getNextMigration finds the next unapplied migration
func getNextMigration(ctx context.Context, client *redisClientWrapper) (migration.Migration, error) {
	migrations := registry.GetAll()

	// Sort migrations by version
	var sortedMigrations []migration.Migration
	for _, m := range migrations {
		sortedMigrations = append(sortedMigrations, m)
	}
	sort.Slice(sortedMigrations, func(i, j int) bool {
		return sortedMigrations[i].Version() < sortedMigrations[j].Version()
	})

	// Find first unapplied
	for _, m := range sortedMigrations {
		if !isApplied(ctx, client, m.Name()) {
			return m, nil
		}
	}

	return nil, fmt.Errorf("all migrations have been applied")
}

// getLastAppliedMigration finds the most recently applied migration
func getLastAppliedMigration(ctx context.Context, client *redisClientWrapper) (migration.Migration, error) {
	migrations := registry.GetAll()

	// Sort migrations by version (descending)
	var sortedMigrations []migration.Migration
	for _, m := range migrations {
		sortedMigrations = append(sortedMigrations, m)
	}
	sort.Slice(sortedMigrations, func(i, j int) bool {
		return sortedMigrations[i].Version() > sortedMigrations[j].Version()
	})

	// Find last applied
	for _, m := range sortedMigrations {
		if isApplied(ctx, client, m.Name()) {
			return m, nil
		}
	}

	return nil, fmt.Errorf("no migrations have been applied")
}

// setMigrationAsApplied marks a migration as applied
func setMigrationAsApplied(ctx context.Context, client *redisClientWrapper, name string) error {
	key := fmt.Sprintf("outpost:migration:%s", name)

	// Use Redis hash to store migration state
	return client.HSet(ctx, key,
		"status", "applied",
		"applied_at", time.Now().Format(time.RFC3339),
	).Err()
}

// redisClientWrapper wraps go-redis Cmdable to implement the redis.Client interface
type redisClientWrapper struct {
	redis.Cmdable
}

func (r *redisClientWrapper) Close() error {
	// go-redis Cmdable doesn't have a Close method
	// This is a no-op for compatibility
	return nil
}
