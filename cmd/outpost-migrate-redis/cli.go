package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/version"
	"github.com/urfave/cli/v3"
)

// NewCommand creates and configures the CLI command
func NewCommand() *cli.Command {
	return &cli.Command{
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
				Action: listMigrationsCommand,
			},
			{
				Name:  "status",
				Usage: "Show current migration status",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:  "current",
						Usage: "Exit with code 1 if migrations are pending (for scripting)",
					},
				},
				Action: statusCommand,
			},
			{
				Name:   "plan",
				Usage:  "Show what changes would be made without applying them",
				Action: planMigrationCommand,
			},
			{
				Name:   "apply",
				Usage:  "Apply the migration",
				Action: applyMigrationCommand,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			// Default action: show help
			return cli.ShowAppHelp(c)
		},
	}
}

// Command handlers that delegate to migration logic
func listMigrationsCommand(ctx context.Context, c *cli.Command) error {
	return ListMigrations()
}

func statusCommand(ctx context.Context, c *cli.Command) error {
	return withConfig(ctx, c, func(ctx context.Context, cfg *config.Config, migrationName string) error {
		// migrationName is not used for status, but withConfig provides it
		return ShowStatus(ctx, cfg, c.Bool("current"), c.Bool("verbose"))
	})
}

func planMigrationCommand(ctx context.Context, c *cli.Command) error {
	return withConfig(ctx, c, func(ctx context.Context, cfg *config.Config, migrationName string) error {
		return PlanMigration(ctx, cfg, migrationName, c.Bool("verbose"))
	})
}

func applyMigrationCommand(ctx context.Context, c *cli.Command) error {
	return withConfig(ctx, c, func(ctx context.Context, cfg *config.Config, migrationName string) error {
		return ApplyMigration(ctx, cfg, migrationName, MigrationOptions{
			Verbose:     c.Bool("verbose"),
			Force:       c.Bool("force"),
			AutoApprove: c.Bool("auto-approve"),
		})
	})
}

// withConfig is a helper that loads config, prints it if verbose, and gets the migration name
func withConfig(ctx context.Context, c *cli.Command, fn func(context.Context, *config.Config, string) error) error {
	cfg, err := loadAndValidateConfig(c)
	if err != nil {
		return err
	}

	if c.Bool("verbose") {
		printRedisConfig(&cfg.Redis)
	}

	migrationName := c.String("migration")
	if migrationName == "" {
		migrationName = "001_hash_tags" // default migration
	}

	return fn(ctx, cfg, migrationName)
}

// loadAndValidateConfig loads config from files/env and applies CLI overrides
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

// printRedisConfig prints the Redis configuration (with password masked)
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
