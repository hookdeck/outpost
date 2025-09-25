package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration"
	migration_001 "github.com/hookdeck/outpost/cmd/outpost-migrate-redis/migration/001_hash_tags"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/version"
	"github.com/urfave/cli/v3"
)

// Simple OSInterface implementation for config loading
type defaultOSImpl struct{}

func (d *defaultOSImpl) Getenv(key string) string {
	return os.Getenv(key)
}

func (d *defaultOSImpl) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (d *defaultOSImpl) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (d *defaultOSImpl) Environ() []string {
	return os.Environ()
}

func main() {
	app := &cli.Command{
		Name:    "outpost migrate redis",
		Usage:   "Redis migration tool for Outpost",
		Version: version.Version(),
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file",
				Sources: cli.EnvVars("CONFIG"),
			},
			&cli.StringFlag{
				Name:    "redis-host",
				Usage:   "Redis server hostname (overrides config)",
				Sources: cli.EnvVars("REDIS_HOST"),
			},
			&cli.StringFlag{
				Name:    "redis-port",
				Usage:   "Redis server port (overrides config)",
				Sources: cli.EnvVars("REDIS_PORT"),
			},
			&cli.StringFlag{
				Name:    "redis-password",
				Usage:   "Redis password (overrides config)",
				Sources: cli.EnvVars("REDIS_PASSWORD"),
			},
			&cli.IntFlag{
				Name:    "redis-database",
				Usage:   "Redis database number (overrides config)",
				Sources: cli.EnvVars("REDIS_DATABASE"),
			},
			&cli.BoolFlag{
				Name:    "redis-cluster",
				Usage:   "Enable Redis cluster mode (overrides config)",
				Sources: cli.EnvVars("REDIS_CLUSTER_ENABLED"),
			},
			&cli.BoolFlag{
				Name:    "redis-tls",
				Usage:   "Enable TLS for Redis connection (overrides config)",
				Sources: cli.EnvVars("REDIS_TLS_ENABLED"),
			},
			&cli.StringFlag{
				Name:    "migration",
				Aliases: []string{"m"},
				Usage:   "Specific migration to run (e.g., '001_hash_tags')",
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "Enable verbose output",
			},
			&cli.BoolFlag{
				Name:    "force",
				Aliases: []string{"f"},
				Usage:   "Skip confirmation prompts",
			},
			&cli.BoolFlag{
				Name:    "auto-approve",
				Aliases: []string{"y"},
				Usage:   "Auto-approve all operations (plan and apply)",
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "List available migrations",
				Action: listMigrations,
			},
			{
				Name:   "plan",
				Usage:  "Show what changes would be made without applying them",
				Action: planMigration,
			},
			{
				Name:   "apply",
				Usage:  "Apply the migration",
				Action: applyMigration,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			// Default action: show help
			return cli.ShowAppHelp(c)
		},
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func loadAndValidateConfig(c *cli.Command) (*config.Config, error) {
	// Load config using the existing system
	flags := config.Flags{
		Config: c.String("config"),
	}

	// Use ParseWithoutValidation to load config files and env vars
	// without validating all required fields (since we only need Redis config)
	cfg, err := config.ParseWithoutValidation(flags, &defaultOSImpl{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Override Redis config with CLI flags if provided
	if host := c.String("redis-host"); host != "" {
		cfg.Redis.Host = host
	}
	if portStr := c.String("redis-port"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("invalid redis-port: %w", err)
		}
		cfg.Redis.Port = port
	}
	if password := c.String("redis-password"); password != "" {
		cfg.Redis.Password = password
	}
	if c.IsSet("redis-database") {
		cfg.Redis.Database = c.Int("redis-database")
	}
	if c.IsSet("redis-cluster") {
		cfg.Redis.ClusterEnabled = c.Bool("redis-cluster")
	}
	if c.IsSet("redis-tls") {
		cfg.Redis.TLSEnabled = c.Bool("redis-tls")
	}

	// Validate Redis configuration
	if err := validateRedisConfig(&cfg.Redis); err != nil {
		return nil, fmt.Errorf("invalid Redis configuration: %w", err)
	}

	return cfg, nil
}

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

func printRedisConfig(rc *config.RedisConfig) {
	fmt.Println("Redis Configuration:")
	fmt.Printf("  Host: %s\n", rc.Host)
	fmt.Printf("  Port: %d\n", rc.Port)
	fmt.Printf("  Database: %d\n", rc.Database)
	fmt.Printf("  Cluster Enabled: %v\n", rc.ClusterEnabled)
	fmt.Printf("  TLS Enabled: %v\n", rc.TLSEnabled)

	// Mask password for security
	if rc.Password != "" {
		fmt.Printf("  Password: ****** (length: %d)\n", len(rc.Password))
	} else {
		fmt.Printf("  Password: (not set)\n")
	}

	if rc.ClusterEnabled && rc.DevClusterHostOverride {
		fmt.Printf("  Dev Cluster Host Override: %v (WARNING: Dev only!)\n", rc.DevClusterHostOverride)
	}
	fmt.Println()
}

func listMigrations(ctx context.Context, c *cli.Command) error {
	fmt.Println("Available migrations:")
	fmt.Println("  001_hash_tags - Add hash tags to keys for Redis Cluster compatibility")
	return nil
}

func planMigration(ctx context.Context, c *cli.Command) error {
	cfg, err := loadAndValidateConfig(c)
	if err != nil {
		return err
	}

	// Print config in verbose mode
	if c.Bool("verbose") {
		printRedisConfig(&cfg.Redis)
	}

	// Build Redis client using ToConfig() method
	redisConfig := cfg.Redis.ToConfig()
	redisClient, err := redis.New(context.Background(), redisConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}
	// Note: go-redis Cmdable doesn't have a Close method, but that's ok

	// Get migration name
	migrationName := c.String("migration")
	if migrationName == "" {
		migrationName = "001_hash_tags" // default migration
	}

	// Run the migration in plan mode
	m, err := getMigration(migrationName)
	if err != nil {
		return err
	}

	// Create a wrapper client that implements the expected interface
	client := &redisClientWrapper{
		Cmdable: redisClient,
	}

	plan, err := m.Plan(context.Background(), client, c.Bool("verbose"))
	if err != nil {
		return err
	}

	// Display the plan
	fmt.Printf("Migration Plan for %s:\n", migrationName)
	fmt.Printf("  Description: %s\n", plan.Description)
	fmt.Printf("  Estimated items: %d\n", plan.EstimatedItems)
	if plan.Scope != nil && len(plan.Scope) > 0 {
		fmt.Println("  Scope:")
		for key, value := range plan.Scope {
			fmt.Printf("    %s: %d\n", key, value)
		}
	}

	return nil
}

func applyMigration(ctx context.Context, c *cli.Command) error {
	cfg, err := loadAndValidateConfig(c)
	if err != nil {
		return err
	}

	// Print config in verbose mode
	if c.Bool("verbose") {
		printRedisConfig(&cfg.Redis)
	}

	// Build Redis client using ToConfig() method
	redisConfig := cfg.Redis.ToConfig()
	redisClient, err := redis.New(context.Background(), redisConfig)
	if err != nil {
		return fmt.Errorf("failed to connect to Redis: %w", err)
	}

	// Get migration name
	migrationName := c.String("migration")
	if migrationName == "" {
		migrationName = "001_hash_tags" // default migration
	}

	// Run the migration
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
	plan, err := m.Plan(context.Background(), client, c.Bool("verbose"))
	if err != nil {
		return err
	}

	fmt.Printf("  Description: %s\n", plan.Description)
	fmt.Printf("  Estimated items: %d\n", plan.EstimatedItems)

	// Confirm if not auto-approved
	if !c.Bool("auto-approve") && !c.Bool("force") {
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
	state, err := m.Apply(context.Background(), client, plan, c.Bool("verbose"))
	if err != nil {
		return err
	}

	fmt.Printf("Migration completed successfully.\n")
	fmt.Printf("  Processed items: %d\n", state.Progress.ProcessedItems)
	fmt.Printf("  Failed items: %d\n", state.Progress.FailedItems)
	fmt.Printf("  Skipped items: %d\n", state.Progress.SkippedItems)
	return nil
}

func getMigration(name string) (migration.Migration, error) {
	switch name {
	case "001_hash_tags", "001", "hash_tags":
		return migration_001.New(), nil
	default:
		return nil, fmt.Errorf("unknown migration: %s", name)
	}
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
