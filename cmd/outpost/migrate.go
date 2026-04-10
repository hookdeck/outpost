package main

import (
	"context"
	"fmt"
	"os"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/migrator/coordinator"
	"github.com/hookdeck/outpost/internal/migrator/migrations"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/urfave/cli/v3"
)

// newMigrateCommand builds the `outpost migrate` subcommand tree. It
// replaces the previous implementation that simply delegated every
// invocation to the outpost-migrate-redis binary.
func newMigrateCommand() *cli.Command {
	return &cli.Command{
		Name:  "migrate",
		Usage: "Database migration tools",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "config",
				Aliases: []string{"c"},
				Usage:   "Path to config file",
				Sources: cli.EnvVars("CONFIG"),
			},
			&cli.BoolFlag{
				Name:  "verbose",
				Usage: "Enable verbose logging",
			},
		},
		Commands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "List all migrations (SQL + Redis) with their status",
				Action: runMigrateList,
			},
			{
				Name:   "plan",
				Usage:  "Show what migrations would be applied",
				Action: runMigratePlan,
			},
			{
				Name:  "apply",
				Usage: "Apply all pending migrations (SQL then Redis)",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "yes",
						Aliases: []string{"y"},
						Usage:   "Skip confirmation prompt",
					},
					&cli.BoolFlag{
						Name:  "sql-only",
						Usage: "Only apply SQL migrations",
					},
					&cli.BoolFlag{
						Name:  "redis-only",
						Usage: "Only apply Redis migrations",
					},
				},
				Action: runMigrateApply,
			},
			{
				Name:   "verify",
				Usage:  "Verify that migrations were applied correctly",
				Action: runMigrateVerify,
			},
			{
				Name:   "unlock",
				Usage:  "Force clear the Redis migration lock (use with caution)",
				Flags: []cli.Flag{
					&cli.BoolFlag{
						Name:    "yes",
						Aliases: []string{"y"},
						Usage:   "Skip confirmation prompt",
					},
				},
				Action: runMigrateUnlock,
			},
		},
		Action: func(ctx context.Context, c *cli.Command) error {
			return cli.ShowSubcommandHelp(c)
		},
	}
}

// withCoordinator loads config, constructs all the migration subsystem
// inputs, builds a Coordinator, and invokes fn. It centralizes setup so
// each subcommand action stays short.
func withCoordinator(ctx context.Context, c *cli.Command, fn func(*coordinator.Coordinator) error) error {
	configPath := c.String("config")
	verbose := c.Bool("verbose")

	cfg, err := config.Parse(config.Flags{Config: configPath})
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logLevel := "info"
	if verbose {
		logLevel = "debug"
	}
	logger, err := logging.NewLogger(logging.WithLogLevel(logLevel))
	if err != nil {
		return fmt.Errorf("create logger: %w", err)
	}

	// SQL migrator is optional — construct it only if a database URL is
	// configured. Config validation happens later, so we tolerate missing
	// SQL configuration and just leave sqlMigrator nil.
	var sqlMigrator *migrator.Migrator
	opts := cfg.ToMigratorOpts()
	if opts.PG.URL != "" || opts.CH.Addr != "" {
		sqlMigrator, err = migrator.New(opts)
		if err != nil {
			return fmt.Errorf("create sql migrator: %w", err)
		}
		defer func() {
			if sourceErr, dbErr := sqlMigrator.Close(ctx); sourceErr != nil || dbErr != nil {
				logger.Warn("failed to close sql migrator")
			}
		}()
	}

	// Redis client + migrations.
	redisClient, err := redis.New(ctx, cfg.Redis.ToConfig())
	if err != nil {
		return fmt.Errorf("connect to redis: %w", err)
	}
	defer redisClient.Close()

	redisMigrations := migrations.AllRedisMigrationsWithLogging(
		redisClient, logger, verbose, cfg.DeploymentID,
	)

	coord := coordinator.New(coordinator.Config{
		SQLMigrator:     sqlMigrator,
		RedisClient:     redisClient,
		RedisMigrations: redisMigrations,
		DeploymentID:    cfg.DeploymentID,
		Logger:          logger,
	})

	return fn(coord)
}

func runMigrateList(ctx context.Context, c *cli.Command) error {
	return withCoordinator(ctx, c, func(coord *coordinator.Coordinator) error {
		list, err := coord.List(ctx)
		if err != nil {
			return err
		}
		printMigrationList(os.Stdout, list)
		return nil
	})
}

func runMigratePlan(ctx context.Context, c *cli.Command) error {
	return withCoordinator(ctx, c, func(coord *coordinator.Coordinator) error {
		plan, err := coord.Plan(ctx)
		if err != nil {
			return err
		}
		printMigrationPlan(os.Stdout, plan)
		return nil
	})
}

func runMigrateApply(ctx context.Context, c *cli.Command) error {
	return withCoordinator(ctx, c, func(coord *coordinator.Coordinator) error {
		plan, err := coord.Plan(ctx)
		if err != nil {
			return err
		}

		if !plan.HasChanges() {
			fmt.Fprintln(os.Stdout, "All migrations are up to date.")
			return nil
		}

		printMigrationPlan(os.Stdout, plan)

		if !c.Bool("yes") {
			fmt.Fprint(os.Stdout, "\nApply these migrations? [y/N]: ")
			var response string
			if _, err := fmt.Fscanln(os.Stdin, &response); err != nil {
				// Fscanln errors on empty input; treat as cancellation.
				fmt.Fprintln(os.Stdout, "Cancelled.")
				return nil
			}
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Fprintln(os.Stdout, "Cancelled.")
				return nil
			}
		}

		opts := coordinator.ApplyOptions{
			SQLOnly:   c.Bool("sql-only"),
			RedisOnly: c.Bool("redis-only"),
		}
		if err := coord.Apply(ctx, opts); err != nil {
			return err
		}

		fmt.Fprintln(os.Stdout, "\nAll migrations applied successfully.")
		return nil
	})
}

func runMigrateVerify(ctx context.Context, c *cli.Command) error {
	return withCoordinator(ctx, c, func(coord *coordinator.Coordinator) error {
		report, err := coord.Verify(ctx)
		if err != nil {
			return err
		}
		printVerificationReport(os.Stdout, report)
		if !report.Ok() {
			return fmt.Errorf("verification failed")
		}
		return nil
	})
}

func runMigrateUnlock(ctx context.Context, c *cli.Command) error {
	return withCoordinator(ctx, c, func(coord *coordinator.Coordinator) error {
		if !c.Bool("yes") {
			fmt.Fprintln(os.Stdout,
				"Warning: clearing the lock while a migration is running can cause data corruption.")
			fmt.Fprint(os.Stdout, "Continue? [y/N]: ")
			var response string
			if _, err := fmt.Fscanln(os.Stdin, &response); err != nil {
				fmt.Fprintln(os.Stdout, "Cancelled.")
				return nil
			}
			if response != "y" && response != "Y" && response != "yes" {
				fmt.Fprintln(os.Stdout, "Cancelled.")
				return nil
			}
		}
		if err := coord.Unlock(ctx); err != nil {
			return err
		}
		fmt.Fprintln(os.Stdout, "Redis migration lock cleared.")
		return nil
	})
}
