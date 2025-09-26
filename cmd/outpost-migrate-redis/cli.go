package main

import (
	"context"

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
	verbose := c.Bool("verbose")
	loader := NewConfigLoader()
	cfg, err := loader.LoadConfig(c)
	if err != nil {
		return nil, err
	}

	if verbose {
		loader.printRedisConfig(&cfg.Redis)
	}

	return NewMigrator(cfg, verbose)
}
