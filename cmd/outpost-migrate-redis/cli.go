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
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "Enable verbose output",
			},
		},
		Commands: []*cli.Command{
			{
				Name:  "list",
				Usage: "List available migrations",
				Action: func(ctx context.Context, c *cli.Command) error {
					return ListMigrations()
				},
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
				Action: func(ctx context.Context, c *cli.Command) error {
					migrator, err := initMigrator(c)
					if err != nil {
						return err
					}
					return migrator.Status(ctx, c.Bool("current"))
				},
			},
			{
				Name:  "plan",
				Usage: "Show what changes would be made without applying them",
				Action: func(ctx context.Context, c *cli.Command) error {
					migrator, err := initMigrator(c)
					if err != nil {
						return err
					}
					return migrator.Plan(ctx)
				},
			},
			{
				Name:  "apply",
				Usage: "Apply the migration",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "yes",
						Aliases: []string{"y"},
						Usage:   "Skip confirmation prompt",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					migrator, err := initMigrator(c)
					if err != nil {
						return err
					}
					return migrator.Apply(ctx, c.Bool("yes"))
				},
			},
			{
				Name:  "verify",
				Usage: "Verify that a migration was successful",
				Action: func(ctx context.Context, c *cli.Command) error {
					migrator, err := initMigrator(c)
					if err != nil {
						return err
					}
					return migrator.Verify(ctx)
				},
			},
			{
				Name:  "cleanup",
				Usage: "Remove old keys after successful migration",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "force",
						Aliases: []string{"f"},
						Usage:   "Skip verification before cleanup",
					},
					&cli.BoolFlag{
						Name:    "yes",
						Aliases: []string{"y"},
						Usage:   "Skip confirmation prompt",
					},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					migrator, err := initMigrator(c)
					if err != nil {
						return err
					}
					return migrator.Cleanup(ctx, c.Bool("force"), c.Bool("yes"))
				},
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			// Default action: show help
			return cli.ShowAppHelp(c)
		},
	}
}

// initMigrator creates a migrator instance from command context
func initMigrator(c *cli.Command) (*Migrator, error) {
	cfg, err := loadAndValidateConfig(c)
	if err != nil {
		return nil, err
	}

	if c.Bool("verbose") {
		printRedisConfig(&cfg.Redis)
	}

	return NewMigrator(cfg, c.Bool("verbose"))
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
