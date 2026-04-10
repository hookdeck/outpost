package app

import (
	"context"
	"fmt"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/migrator/coordinator"
	"github.com/hookdeck/outpost/internal/migrator/migrations"
	"github.com/hookdeck/outpost/internal/redis"
	"go.uber.org/zap"
)

// checkPendingMigrations inspects both the SQL and Redis migration
// subsystems through a unified coordinator and returns an error if any
// migrations are pending.
//
// This replaces the previous behavior where runMigration() and
// runRedisMigrations() silently applied migrations at startup. The new
// rule is explicit: operators must run `outpost migrate apply` before
// starting the server. If the check finds pending migrations, the
// application refuses to start so a half-migrated deployment cannot
// accidentally come online.
func checkPendingMigrations(
	ctx context.Context,
	cfg *config.Config,
	redisClient redis.Client,
	logger *logging.Logger,
) error {
	opts := cfg.ToMigratorOpts()

	var sqlMigrator *migrator.Migrator
	if opts.PG.URL != "" || opts.CH.Addr != "" {
		m, err := migrator.New(opts)
		if err != nil {
			return fmt.Errorf("create sql migrator for pending check: %w", err)
		}
		defer func() {
			if sourceErr, dbErr := m.Close(ctx); sourceErr != nil || dbErr != nil {
				logger.Warn("failed to close sql migrator during pending check",
					zap.NamedError("source_err", sourceErr),
					zap.NamedError("db_err", dbErr))
			}
		}()
		sqlMigrator = m
	}

	redisMigrations := migrations.AllRedisMigrationsWithLogging(
		redisClient, logger, false, cfg.DeploymentID,
	)

	coord := coordinator.New(coordinator.Config{
		SQLMigrator:     sqlMigrator,
		RedisClient:     redisClient,
		RedisMigrations: redisMigrations,
		DeploymentID:    cfg.DeploymentID,
		Logger:          logger,
	})

	summary, err := coord.PendingSummary(ctx)
	if err != nil {
		return fmt.Errorf("check pending migrations: %w", err)
	}

	if !summary.HasPending() {
		logger.Info("no pending migrations")
		return nil
	}

	logger.Error("pending migrations detected, refusing to start",
		zap.Int("sql_pending", summary.SQLPending),
		zap.Int("redis_pending", summary.RedisPending))

	return fmt.Errorf(
		"pending migrations detected (%d SQL, %d Redis) — run 'outpost migrate apply' before starting the server",
		summary.SQLPending, summary.RedisPending,
	)
}
