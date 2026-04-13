package configs

import (
	"context"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/migrator/coordinator"
	"github.com/hookdeck/outpost/internal/migrator/migrations"
	"github.com/hookdeck/outpost/internal/redis"
)

// ApplyMigrations runs all pending SQL and Redis migrations for the given
// config. E2E tests must call this before starting the app, since the app
// refuses to start when migrations are pending.
func ApplyMigrations(t *testing.T, cfg *config.Config) {
	t.Helper()
	ctx := context.Background()

	logger, err := logging.NewLogger(logging.WithLogLevel("warn"))
	if err != nil {
		t.Fatalf("create logger for migrations: %v", err)
	}

	// SQL migrator
	opts := cfg.ToMigratorOpts()
	var sqlMigrator *migrator.Migrator
	if opts.PG.URL != "" || opts.CH.Addr != "" {
		m, err := migrator.New(opts)
		if err != nil {
			t.Fatalf("create sql migrator: %v", err)
		}
		defer func() {
			if sourceErr, dbErr := m.Close(ctx); sourceErr != nil || dbErr != nil {
				t.Logf("close sql migrator: source=%v db=%v", sourceErr, dbErr)
			}
		}()
		sqlMigrator = m
	}

	// Redis client
	redisClient, err := redis.New(ctx, cfg.Redis.ToConfig())
	if err != nil {
		t.Fatalf("create redis client for migrations: %v", err)
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

	if err := coord.Apply(ctx, coordinator.ApplyOptions{}); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
}
