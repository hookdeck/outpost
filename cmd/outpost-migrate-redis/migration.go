package main

import (
	"context"
	"errors"
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
	client      *redisClientWrapper
	verbose     bool
	force       bool
	autoApprove bool
}

// NewMigrator creates a new migrator instance
func NewMigrator(cfg *config.Config, verbose, force, autoApprove bool) (*Migrator, error) {
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
		client:      client,
		verbose:     verbose,
		force:       force,
		autoApprove: autoApprove,
	}, nil
}

// ListMigrations lists all available migrations
func ListMigrations() error {
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
		m := migrations[name]
		fmt.Printf("  %s - %s\n", name, m.Description())
	}
	return nil
}

// Status shows the current migration status
func (m *Migrator) Status(ctx context.Context, currentCheck bool) error {

	// Get all migrations and categorize them
	migrations := registry.GetAll()
	var appliedMigrations []migration.Migration
	var pendingMigrations []migration.Migration

	// Collect all migrations and sort them by version
	var allMigrations []migration.Migration
	for _, m := range migrations {
		allMigrations = append(allMigrations, m)
	}

	// Sort migrations by version
	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].Version() < allMigrations[j].Version()
	})

	// Categorize migrations
	for _, mig := range allMigrations {
		if isApplied(ctx, m.client, mig.Name()) {
			appliedMigrations = append(appliedMigrations, mig)
		} else {
			pendingMigrations = append(pendingMigrations, mig)
		}
	}

	// If --current flag is used, just check and exit
	if currentCheck {
		if len(pendingMigrations) > 0 {
			if !m.verbose {
				// Silent mode for scripting
				os.Exit(1)
			}
			fmt.Fprintf(os.Stderr, "Migration required: %d pending\n", len(pendingMigrations))
			os.Exit(1)
		}
		// Up to date
		return nil
	}

	// Display status information
	fmt.Println("Migration Status:")

	if len(appliedMigrations) == 0 {
		fmt.Println("  No migrations applied")
	} else {
		fmt.Printf("  Applied: %d migration(s)\n", len(appliedMigrations))
		if m.verbose {
			for _, mig := range appliedMigrations {
				fmt.Printf("    ✓ %s\n", mig.Name())
			}
		}
	}

	if len(pendingMigrations) == 0 {
		fmt.Println("  Status: Up to date")
	} else {
		fmt.Printf("  Pending: %d migration(s)\n", len(pendingMigrations))
		for _, m := range pendingMigrations {
			fmt.Printf("    • %s - %s\n", m.Name(), m.Description())
		}
		fmt.Printf("\nNext migration: %s\n", pendingMigrations[0].Name())
		fmt.Println("Run 'outpost migrate redis plan' to preview changes")
	}

	return nil
}


// Plan shows what changes would be made without applying them
func (m *Migrator) Plan(ctx context.Context) error {
	// Get the next unapplied migration
	mig, err := getNextMigration(ctx, m.client)
	if err != nil {
		if err.Error() == "all migrations have been applied" {
			fmt.Println("All migrations have been applied. Nothing to plan.")
			return nil
		}
		return err
	}

	// Run the migration in plan mode
	plan, err := mig.Plan(ctx, m.client, m.verbose)
	if err != nil {
		return err
	}

	// Display the plan
	fmt.Printf("Migration Plan for %s:\n", mig.Name())
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
func (m *Migrator) Cleanup(ctx context.Context) error {
	// Get the last applied migration
	mig, err := getLastAppliedMigration(ctx, m.client)
	if err != nil {
		return err
	}

	// First verify the migration if not forced
	if !m.force {
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
	if !m.autoApprove && !m.force {
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

	fmt.Printf("\nCleaning up old keys from migration %s...\n", mig.Name())

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
func (m *Migrator) Apply(ctx context.Context) error {
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
	if !m.autoApprove && !m.force {
		fmt.Print("\nDo you want to apply these changes? (yes/no): ")
		var response string
		fmt.Scanln(&response)
		if !strings.HasPrefix(strings.ToLower(response), "y") {
			fmt.Println("Migration cancelled.")
			return nil
		}
	}

	// Apply the migration
	fmt.Println("\nApplying migration...")
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

// validateRedisConfig validates the Redis configuration
func validateRedisConfig(rc *config.RedisConfig) error {
	// Basic validation for Redis config
	if rc.Host == "" {
		return errors.New("redis host is required")
	}
	if rc.Port == 0 {
		return errors.New("redis port is required")
	}

	// Check for cluster-specific configuration
	if rc.ClusterEnabled {
		if rc.Database != 0 {
			return errors.New("redis cluster mode doesn't support database selection")
		}
	}

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
