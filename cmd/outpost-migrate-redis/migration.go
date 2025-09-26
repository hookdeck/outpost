package main

import (
	"context"
	"errors"
	"fmt"
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

// redisClientWrapper wraps go-redis Cmdable to implement the redis.Client interface
type redisClientWrapper struct {
	redis.Cmdable
}

func (r *redisClientWrapper) Close() error {
	// go-redis Cmdable doesn't have a Close method
	// This is a no-op for compatibility
	return nil
}
