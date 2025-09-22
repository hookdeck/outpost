package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hookdeck/outpost/cmd/migrateredis/migration"
	migration_001 "github.com/hookdeck/outpost/cmd/migrateredis/migration/001_hash_tags"
	"github.com/hookdeck/outpost/internal/redis"
)

var (
	// Global flags - these will be initialized in main() after loading .env
	redisHost     *string
	redisPort     *string
	redisPassword *string
	redisDatabase *int
	redisCluster  *bool
	redisTLS      *bool
	migrationName *string
	verbose       *bool
	force         *bool
	help          *bool
)

var registry *migration.Registry

func init() {
	// Register all available migrations
	registry = migration.NewRegistry()
	registry.Register(migration_001.New())
	// Future migrations can be registered here:
	// registry.Register(migration_002.New())
	// registry.Register(migration_003.New())
}

func main() {
	// Load .env file if Redis env vars aren't already set
	if shouldLoadEnvFile() {
		loadEnvFile()
	}

	// Initialize flags after loading .env so defaults come from env
	redisHost = flag.String("redis-host", getEnvOrDefault("REDIS_HOST", "localhost"), "Redis server hostname")
	redisPort = flag.String("redis-port", getEnvOrDefault("REDIS_PORT", "6379"), "Redis server port")
	redisPassword = flag.String("redis-password", getEnvOrDefault("REDIS_PASSWORD", ""), "Redis password for authentication")
	redisDatabase = flag.Int("redis-database", getEnvOrDefaultInt("REDIS_DATABASE", 0), "Redis database number (ignored in cluster mode)")
	redisCluster = flag.Bool("redis-cluster-enabled", getEnvOrDefault("REDIS_CLUSTER_ENABLED", "false") == "true", "Enable Redis cluster mode")
	redisTLS = flag.Bool("redis-tls-enabled", getEnvOrDefault("REDIS_TLS_ENABLED", "false") == "true", "Enable TLS encryption for Redis connection")
	migrationName = flag.String("migration", "", "Migration to run (e.g., 001_hash_tags)")
	verbose = flag.Bool("verbose", false, "Enable verbose output")
	force = flag.Bool("force", false, "Force cleanup without confirmation")
	help = flag.Bool("help", false, "Show help message")

	// Find and extract command from args before parsing flags
	var command string
	args := os.Args[1:] // Skip program name

	// Look for the command (last non-flag argument)
	for i := len(args) - 1; i >= 0; i-- {
		if !strings.HasPrefix(args[i], "-") {
			command = args[i]
			// Remove command from os.Args before parsing flags
			os.Args = append(os.Args[:i+1], os.Args[i+2:]...)
			break
		}
	}

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Outpost Redis Migration Tool - Migrate Redis keys between schema versions\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n")
		fmt.Fprintf(os.Stderr, "  migrateredis [options] <command>\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  list      List available migrations\n")
		fmt.Fprintf(os.Stderr, "  plan      Analyze and plan the migration (dry-run)\n")
		fmt.Fprintf(os.Stderr, "  apply     Execute the migration (preserves old keys)\n")
		fmt.Fprintf(os.Stderr, "  verify    Verify migration was successful\n")
		fmt.Fprintf(os.Stderr, "  cleanup   Remove old keys after verification\n")
		fmt.Fprintf(os.Stderr, "  status    Check current migration status\n")
		fmt.Fprintf(os.Stderr, "  unlock    Clear stuck migration lock (use with caution)\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nAvailable Migrations:\n")
		for _, name := range registry.List() {
			if m, ok := registry.Get(name); ok {
				fmt.Fprintf(os.Stderr, "  %s\n    %s\n", name, m.Description())
			}
		}
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  # List available migrations\n")
		fmt.Fprintf(os.Stderr, "  migrateredis list\n\n")
		fmt.Fprintf(os.Stderr, "  # Plan migration (dry-run)\n")
		fmt.Fprintf(os.Stderr, "  migrateredis -migration 001_hash_tags plan\n\n")
		fmt.Fprintf(os.Stderr, "  # Apply migration\n")
		fmt.Fprintf(os.Stderr, "  migrateredis -migration 001_hash_tags apply\n\n")
		fmt.Fprintf(os.Stderr, "  # Verify and cleanup\n")
		fmt.Fprintf(os.Stderr, "  migrateredis -migration 001_hash_tags verify\n")
		fmt.Fprintf(os.Stderr, "  migrateredis -migration 001_hash_tags -force cleanup\n")
	}

	// Parse flags now
	flag.Parse()

	// Check for help or no command
	if *help || (command == "" && len(os.Args) < 2) {
		flag.Usage()
		os.Exit(0)
	}

	// If we didn't find a command, error
	if command == "" {
		fmt.Fprintf(os.Stderr, "Error: No command specified\n")
		fmt.Fprintf(os.Stderr, "Usage: migrateredis [options] <command>\n")
		fmt.Fprintf(os.Stderr, "Example: migrateredis -migration 001_hash_tags plan\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Handle list command
	if command == "list" {
		listMigrations()
		return
	}

	// For other commands, ensure migration is specified (except unlock)
	if command != "status" && command != "unlock" && *migrationName == "" {
		fmt.Fprintf(os.Stderr, "Error: -migration flag is required\n\n")
		flag.Usage()
		os.Exit(1)
	}

	// Get migration
	var mig migration.Migration
	if *migrationName != "" {
		var ok bool
		mig, ok = registry.Get(*migrationName)
		if !ok {
			fmt.Fprintf(os.Stderr, "Error: Unknown migration '%s'\n", *migrationName)
			fmt.Fprintf(os.Stderr, "Available migrations:\n")
			for _, name := range registry.List() {
				fmt.Fprintf(os.Stderr, "  - %s\n", name)
			}
			os.Exit(1)
		}
	}

	// Create Redis client
	client, err := createRedisClient()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to Redis: %v\n", err)
		os.Exit(1)
	}
	defer client.Close()

	ctx := context.Background()

	// Check Redis connection
	if err := client.Ping(ctx).Err(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to ping Redis: %v\n", err)
		os.Exit(1)
	}

	switch command {
	case "plan":
		if err := runPlan(ctx, client, mig); err != nil {
			fmt.Fprintf(os.Stderr, "Plan failed: %v\n", err)
			os.Exit(1)
		}
	case "apply":
		if err := runApply(ctx, client, mig); err != nil {
			fmt.Fprintf(os.Stderr, "Apply failed: %v\n", err)
			os.Exit(1)
		}
	case "verify":
		if err := runVerify(ctx, client, mig); err != nil {
			fmt.Fprintf(os.Stderr, "Verify failed: %v\n", err)
			os.Exit(1)
		}
	case "cleanup":
		if err := runCleanup(ctx, client, mig); err != nil {
			fmt.Fprintf(os.Stderr, "Cleanup failed: %v\n", err)
			os.Exit(1)
		}
	case "status":
		if err := runStatus(ctx, client); err != nil {
			fmt.Fprintf(os.Stderr, "Status check failed: %v\n", err)
			os.Exit(1)
		}
	case "unlock":
		if err := forceClearMigrationLock(ctx, client); err != nil {
			fmt.Fprintf(os.Stderr, "Unlock failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", command)
		flag.Usage()
		os.Exit(1)
	}
}

func listMigrations() {
	fmt.Println("Available Redis Migrations:")
	fmt.Println()

	for _, name := range registry.List() {
		if m, ok := registry.Get(name); ok {
			fmt.Printf("• %s\n", name)
			fmt.Printf("  %s\n\n", m.Description())
		}
	}
}

func runPlan(ctx context.Context, client redis.Client, mig migration.Migration) error {
	fmt.Printf("=== Redis Migration Plan: %s ===\n", mig.Name())
	fmt.Printf("%s\n\n", mig.Description())

	// Check current schema version
	currentVersion, err := getCurrentSchemaVersion(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	targetVersion := mig.Version()
	fmt.Printf("Current schema version: %d\n", currentVersion)
	fmt.Printf("Target schema version: %d\n\n", targetVersion)

	// Check if migration can run
	if !canRunMigration(currentVersion, targetVersion) {
		if currentVersion >= targetVersion {
			fmt.Printf("⚠️  Migration already applied (current version %d >= target version %d)\n", currentVersion, targetVersion)
			return nil
		}
		return fmt.Errorf("cannot run migration: current version is %d, but migration %s requires version %d",
			currentVersion, mig.Name(), targetVersion-1)
	}

	plan, err := mig.Plan(ctx, client, *verbose)
	if err != nil {
		return err
	}

	if plan.EstimatedItems == 0 {
		fmt.Println("✅ No items to migrate. Migration not needed.")
		return nil
	}

	fmt.Printf("Migration scope:\n")
	for key, value := range plan.Scope {
		fmt.Printf("  %-15s: %d\n", key, value)
	}
	fmt.Printf("  %-15s: %s\n", "Est. duration", plan.EstimatedTime)
	fmt.Println()

	fmt.Printf("✅ Plan complete. Run 'migrateredis -migration %s apply' to execute.\n", mig.Name())
	return nil
}

func runApply(ctx context.Context, client redis.Client, mig migration.Migration) error {
	fmt.Printf("=== Applying Redis Migration: %s ===\n", mig.Name())

	// Acquire migration lock
	fmt.Println("Acquiring migration lock...")
	if err := acquireMigrationLock(ctx, client, mig.Name()); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Ensure lock is released on function exit
	defer func() {
		fmt.Println("Releasing migration lock...")
		if err := releaseMigrationLock(ctx, client); err != nil {
			fmt.Printf("⚠️  Warning: failed to release lock: %v\n", err)
			fmt.Printf("You may need to run: migrateredis unlock\n")
		}
	}()

	// Check current schema version
	currentVersion, err := getCurrentSchemaVersion(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to get current schema version: %w", err)
	}

	targetVersion := mig.Version()

	// Verify migration can run
	if !canRunMigration(currentVersion, targetVersion) {
		if currentVersion >= targetVersion {
			fmt.Printf("⚠️  Migration already applied (current version %d >= target version %d)\n", currentVersion, targetVersion)
			return nil
		}
		return fmt.Errorf("cannot run migration: current version is %d, but migration %s requires version %d",
			currentVersion, mig.Name(), targetVersion-1)
	}

	// Generate a fresh plan to see what we're migrating
	fmt.Println("Analyzing current state...")
	plan, err := mig.Plan(ctx, client, false)
	if err != nil {
		return fmt.Errorf("failed to analyze migration scope: %w", err)
	}

	if plan.EstimatedItems == 0 {
		fmt.Println("✅ No items to migrate.")
		return nil
	}

	// Check if already applied
	stateKey := fmt.Sprintf("outpost:migration:%s:state", mig.Name())
	existingStateJSON, _ := client.Get(ctx, stateKey).Result()
	if existingStateJSON != "" {
		var existingState migration.State
		json.Unmarshal([]byte(existingStateJSON), &existingState)
		if existingState.Phase == "applied" || existingState.Phase == "verified" {
			fmt.Printf("⚠️  Migration already %s. Run 'migrateredis verify -migration %s' or 'migrateredis cleanup -migration %s'.\n",
				existingState.Phase, mig.Name(), mig.Name())
			return nil
		}
	}

	fmt.Printf("Applying migration with %d estimated items...\n\n", plan.EstimatedItems)

	// Apply migration
	state, err := mig.Apply(ctx, client, plan, *verbose)
	if err != nil {
		return fmt.Errorf("migration apply failed: %w", err)
	}

	// Save state
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("failed to marshal state: %w", err)
	}

	if err := client.Set(ctx, stateKey, stateJSON, 0).Err(); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}

	fmt.Println()
	fmt.Printf("=== Migration Applied ===\n")
	fmt.Printf("✅ Processed: %d items\n", state.Progress.ProcessedItems)
	if state.Progress.FailedItems > 0 {
		fmt.Printf("❌ Failed: %d items\n", state.Progress.FailedItems)
	}
	fmt.Println()
	fmt.Println("Next steps:")
	fmt.Printf("1. Run 'migrateredis verify -migration %s' to validate migration\n", mig.Name())
	fmt.Println("2. Test your application with the new format")
	fmt.Printf("3. Run 'migrateredis cleanup -migration %s' to remove old keys\n", mig.Name())

	return nil
}

func runVerify(ctx context.Context, client redis.Client, mig migration.Migration) error {
	fmt.Printf("=== Verifying Redis Migration: %s ===\n", mig.Name())

	// Load migration state
	stateKey := fmt.Sprintf("outpost:migration:%s:state", mig.Name())
	stateJSON, err := client.Get(ctx, stateKey).Result()
	if err != nil {
		return fmt.Errorf("no migration state found. Run 'migrateredis apply -migration %s' first", mig.Name())
	}

	var state migration.State
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}

	if state.Phase != "applied" {
		fmt.Printf("Migration is in phase '%s'. Run 'migrateredis apply -migration %s' first.\n", state.Phase, mig.Name())
		return nil
	}

	fmt.Printf("Verifying %d processed items...\n\n", state.Progress.ProcessedItems)

	// Run verification
	result, err := mig.Verify(ctx, client, &state, *verbose)
	if err != nil {
		return fmt.Errorf("verification failed: %w", err)
	}

	if result.Valid {
		fmt.Println("✅ Migration verified successfully!")
		fmt.Printf("Checks passed: %d/%d\n", result.ChecksPassed, result.ChecksRun)
		fmt.Println()
		fmt.Printf("You can now safely run 'migrateredis -migration %s cleanup' to remove old keys.\n", mig.Name())
	} else {
		fmt.Printf("❌ Verification failed!\n")
		fmt.Printf("Checks passed: %d/%d\n", result.ChecksPassed, result.ChecksRun)
		if len(result.Issues) > 0 {
			fmt.Println("\nIssues found:")
			for _, issue := range result.Issues {
				fmt.Printf("  - %s\n", issue)
			}
		}
		fmt.Println("\nPlease investigate before proceeding with cleanup.")
	}

	return nil
}

func runCleanup(ctx context.Context, client redis.Client, mig migration.Migration) error {
	fmt.Printf("=== Redis Migration Cleanup: %s ===\n", mig.Name())

	// Acquire migration lock
	fmt.Println("Acquiring migration lock...")
	if err := acquireMigrationLock(ctx, client, mig.Name()); err != nil {
		return fmt.Errorf("failed to acquire lock: %w", err)
	}

	// Ensure lock is released on function exit
	defer func() {
		fmt.Println("Releasing migration lock...")
		if err := releaseMigrationLock(ctx, client); err != nil {
			fmt.Printf("⚠️  Warning: failed to release lock: %v\n", err)
			fmt.Printf("You may need to run: migrateredis unlock\n")
		}
	}()

	// Load migration state
	stateKey := fmt.Sprintf("outpost:migration:%s:state", mig.Name())
	stateJSON, err := client.Get(ctx, stateKey).Result()
	if err != nil {
		return fmt.Errorf("no migration state found")
	}

	var state migration.State
	if err := json.Unmarshal([]byte(stateJSON), &state); err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}

	if state.Phase != "applied" {
		fmt.Printf("⚠️  Migration not in applied state (current phase: %s).\n", state.Phase)
		if state.Phase == "cleaned" {
			fmt.Println("Migration already cleaned up.")
			return nil
		}
		return fmt.Errorf("migration must be applied before cleanup")
	}

	// Run cleanup
	if err := mig.Cleanup(ctx, client, &state, *force, *verbose); err != nil {
		return err
	}

	// Update state
	state.Phase = "cleaned"
	completed := time.Now()
	state.CompletedAt = &completed
	updatedStateJSON, _ := json.Marshal(state)
	client.Set(ctx, stateKey, updatedStateJSON, 0)

	// Update schema version after successful cleanup
	newVersion := mig.Version()
	if err := setSchemaVersion(ctx, client, newVersion); err != nil {
		return fmt.Errorf("failed to update schema version: %w", err)
	}

	fmt.Printf("\nMigration '%s' is now complete!\n", mig.Name())
	fmt.Printf("Schema version updated to: %d\n", newVersion)
	return nil
}

func runStatus(ctx context.Context, client redis.Client) error {
	fmt.Println("=== Redis Migration Status ===")
	fmt.Println()

	// Get current schema version
	currentVersion, err := getCurrentSchemaVersion(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to get schema version: %w", err)
	}

	fmt.Printf("Current schema version: %d", currentVersion)
	if currentVersion == 0 {
		fmt.Printf(" (original schema)")
	}
	fmt.Println()

	// Check status for all registered migrations
	hasActiveMigration := false

	for _, name := range registry.List() {
		mig, _ := registry.Get(name)
		stateKey := fmt.Sprintf("outpost:migration:%s:state", name)

		targetVersion := mig.Version()

		// Check migration state
		stateJSON, err := client.Get(ctx, stateKey).Result()
		if err == nil {
			var state migration.State
			if err := json.Unmarshal([]byte(stateJSON), &state); err == nil {
				hasActiveMigration = true

				fmt.Printf("Migration: %s (v%d → v%d)\n", name, targetVersion-1, targetVersion)
				fmt.Printf("  Description: %s\n", mig.Description())
				fmt.Printf("  Phase: %s\n", state.Phase)
				fmt.Printf("  Started: %s\n", state.StartedAt.Format(time.RFC3339))
				if state.CompletedAt != nil {
					fmt.Printf("  Completed: %s\n", state.CompletedAt.Format(time.RFC3339))
				}
				fmt.Printf("  Progress: %d processed, %d failed\n",
					state.Progress.ProcessedItems, state.Progress.FailedItems)

				switch state.Phase {
				case "applied":
					fmt.Printf("  Next: Run 'migrateredis -migration %s verify' (optional) or 'cleanup'\n", name)
				case "cleaned":
					fmt.Printf("  Status: ✅ Complete\n")
				}
				fmt.Println()
			}
		} else if canRunMigration(currentVersion, targetVersion) {
			// This is the next migration to run
			fmt.Printf("Next migration available: %s (v%d → v%d)\n", name, currentVersion, targetVersion)
			fmt.Printf("  Description: %s\n", mig.Description())
			fmt.Printf("  Run: migrateredis plan -migration %s\n", name)
			fmt.Println()
		}
	}

	if !hasActiveMigration {
		fmt.Println("No migrations in progress.")

		// Check for legacy keys as a hint
		if mig, ok := registry.Get("001_hash_tags"); ok {
			plan, err := mig.Plan(ctx, client, false)
			if err == nil && plan.EstimatedItems > 0 {
				fmt.Printf("\n⚠️  Found %d items that could be migrated.\n", plan.EstimatedItems)
				fmt.Printf("Consider running: migrateredis plan -migration 001_hash_tags\n")
			}
		}
	}

	return nil
}

func createRedisClient() (redis.Client, error) {
	port := 6379
	if *redisPort != "" {
		fmt.Sscanf(*redisPort, "%d", &port)
	}

	config := &redis.RedisConfig{
		Host:           *redisHost,
		Port:           port,
		Password:       *redisPassword,
		Database:       *redisDatabase,
		ClusterEnabled: *redisCluster,
		TLSEnabled:     *redisTLS,
	}

	// Auto-enable TLS for common ports if not explicitly set
	if !*redisTLS && (port == 6380 || port == 10000) {
		config.TLSEnabled = true
	}

	// Use NewClient which returns Cmdable, then wrap it
	cmdable, err := redis.NewClient(context.Background(), config)
	if err != nil {
		return nil, err
	}

	// The underlying r.Client or r.ClusterClient already implements both Cmdable and Close
	// We just need to assert it to our Client interface
	if client, ok := cmdable.(redis.Client); ok {
		return client, nil
	}

	// If it doesn't implement Client, we need to handle this case
	return nil, fmt.Errorf("redis client does not implement required interface")
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvOrDefaultInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

// shouldLoadEnvFile checks if we need to load .env file
// Returns true if no Redis environment variables are set
func shouldLoadEnvFile() bool {
	redisEnvVars := []string{"REDIS_HOST", "REDIS_PORT", "REDIS_PASSWORD", "REDIS_DATABASE", "REDIS_CLUSTER_ENABLED", "REDIS_TLS_ENABLED"}
	for _, envVar := range redisEnvVars {
		if os.Getenv(envVar) != "" {
			// At least one Redis env var is set, don't load .env
			return false
		}
	}
	return true
}

// loadEnvFile loads environment variables from .env file if it exists
func loadEnvFile() {
	envFile := ".env"

	// Check if .env file exists
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		return // No .env file, nothing to load
	}

	// Read the file
	content, err := os.ReadFile(envFile)
	if err != nil {
		return // Can't read file, skip silently
	}

	// Parse and set environment variables
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		// Skip empty lines and comments
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE format
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		value = strings.Trim(value, `"'`)

		// Only set if not already set (env vars take precedence over .env)
		if os.Getenv(key) == "" {
			log.Println("setting env var", key, value)
			os.Setenv(key, value)
		}
	}
}
