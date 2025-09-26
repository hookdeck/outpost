package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

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

// MigrationOptions contains options for running migrations
type MigrationOptions struct {
	Verbose     bool
	Force       bool
	AutoApprove bool
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

// ShowStatus shows the current migration status
func ShowStatus(ctx context.Context, cfg *config.Config, currentCheck bool, verbose bool) error {
	// Build Redis client
	redisConfig := cfg.Redis.ToConfig()
	redisClient, err := redis.New(ctx, redisConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Create a wrapper client
	client := &redisClientWrapper{
		Cmdable: redisClient,
	}

	// Get current schema version from Redis
	currentVersion, err := getCurrentSchemaVersion(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	// Get all migrations and find the highest version
	migrations := registry.GetAll()
	var highestVersion int
	var pendingMigrations []migration.Migration

	// Collect all migrations and sort them
	var allMigrations []migration.Migration
	for _, m := range migrations {
		allMigrations = append(allMigrations, m)
		if m.Version() > highestVersion {
			highestVersion = m.Version()
		}
	}

	// Sort migrations by version
	sort.Slice(allMigrations, func(i, j int) bool {
		return allMigrations[i].Version() < allMigrations[j].Version()
	})

	// Find pending migrations
	for _, m := range allMigrations {
		if m.Version() > currentVersion {
			pendingMigrations = append(pendingMigrations, m)
		}
	}

	// If --current flag is used, just check and exit
	if currentCheck {
		if len(pendingMigrations) > 0 {
			if !verbose {
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
	fmt.Printf("  Current version: %d\n", currentVersion)
	fmt.Printf("  Latest version:  %d\n", highestVersion)

	if currentVersion == 0 {
		fmt.Println("  Status: Unversioned (no migrations applied)")
	} else if len(pendingMigrations) == 0 {
		fmt.Println("  Status: Up to date")
	} else {
		fmt.Printf("  Status: %d migration(s) pending\n", len(pendingMigrations))
	}

	// Show applied migrations
	if verbose && currentVersion > 0 {
		fmt.Println("\nApplied migrations:")
		for _, m := range allMigrations {
			if m.Version() <= currentVersion {
				state, err := getMigrationState(ctx, client, m.Name())
				if err == nil && state != nil {
					fmt.Printf("  ✓ %s (v%d) - %s\n", m.Name(), m.Version(), state.Phase)
				} else {
					fmt.Printf("  ✓ %s (v%d)\n", m.Name(), m.Version())
				}
			}
		}
	}

	// Show pending migrations
	if len(pendingMigrations) > 0 {
		fmt.Println("\nPending migrations:")
		for _, m := range pendingMigrations {
			fmt.Printf("  • %s (v%d) - %s\n", m.Name(), m.Version(), m.Description())
		}
		fmt.Printf("\nNext migration: %s\n", pendingMigrations[0].Name())
		fmt.Println("Run 'outpost migrate redis plan' to preview changes")
	}

	return nil
}

// getMigrationState retrieves the state of a specific migration from Redis
func getMigrationState(ctx context.Context, client *redisClientWrapper, name string) (*migration.State, error) {
	key := fmt.Sprintf("outpost:migration:%s:state", name)
	val, err := client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, nil
		}
		return nil, err
	}

	// For now, just return a simple state
	// In a real implementation, we'd deserialize the JSON state
	return &migration.State{
		MigrationName: name,
		Phase:         val, // Assuming we store just the phase for simplicity
	}, nil
}

// PlanMigration shows what changes would be made without applying them
func PlanMigration(ctx context.Context, cfg *config.Config, migrationName string, verbose bool) error {
	// Build Redis client
	redisConfig := cfg.Redis.ToConfig()
	redisClient, err := redis.New(ctx, redisConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Get migration
	m, err := getMigration(migrationName)
	if err != nil {
		return err
	}

	// Create a wrapper client that implements the expected interface
	client := &redisClientWrapper{
		Cmdable: redisClient,
	}

	// Run the migration in plan mode
	plan, err := m.Plan(ctx, client, verbose)
	if err != nil {
		return err
	}

	// Display the plan
	fmt.Printf("Migration Plan for %s:\n", migrationName)
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

// ApplyMigration executes the migration
func ApplyMigration(ctx context.Context, cfg *config.Config, migrationName string, opts MigrationOptions) error {
	// Build Redis client
	redisConfig := cfg.Redis.ToConfig()
	redisClient, err := redis.New(ctx, redisConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Get migration
	m, err := getMigration(migrationName)
	if err != nil {
		return err
	}

	// Create a wrapper client
	client := &redisClientWrapper{
		Cmdable: redisClient,
	}

	// First show the plan
	fmt.Println("Planning migration...")
	plan, err := m.Plan(ctx, client, opts.Verbose)
	if err != nil {
		return err
	}

	fmt.Printf("  Description: %s\n", plan.Description)
	fmt.Printf("  Estimated items: %d\n", plan.EstimatedItems)

	// Confirm if not auto-approved
	if !opts.AutoApprove && !opts.Force {
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
	state, err := m.Apply(ctx, client, plan, opts.Verbose)
	if err != nil {
		return err
	}

	// Update schema version in Redis
	if err := setSchemaVersion(ctx, client, m.Version()); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	// Store migration state
	if err := setMigrationState(ctx, client, m.Name(), "applied"); err != nil {
		return fmt.Errorf("failed to update migration state: %w", err)
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

// getMigration returns a migration instance by name
func getMigration(name string) (migration.Migration, error) {
	// Try exact match first
	if m, ok := registry.Get(name); ok {
		return m, nil
	}

	// Try aliases for convenience (e.g., "001" for "001_hash_tags")
	// This allows users to use shorthand
	for _, registeredName := range registry.List() {
		// Check if input is a prefix (e.g., "001" matches "001_hash_tags")
		if strings.HasPrefix(registeredName, name+"_") {
			if m, ok := registry.Get(registeredName); ok {
				return m, nil
			}
		}
		// Check if input is a suffix (e.g., "hash_tags" matches "001_hash_tags")
		parts := strings.SplitN(registeredName, "_", 2)
		if len(parts) == 2 && parts[1] == name {
			if m, ok := registry.Get(registeredName); ok {
				return m, nil
			}
		}
	}

	return nil, fmt.Errorf("unknown migration: %s", name)
}

// setMigrationState stores the state of a specific migration in Redis
func setMigrationState(ctx context.Context, client *redisClientWrapper, name string, phase string) error {
	key := fmt.Sprintf("outpost:migration:%s:state", name)
	// For now, just store the phase. In a real implementation, we'd serialize the full state
	return client.Set(ctx, key, phase, 0).Err()
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
