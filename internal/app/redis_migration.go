package app

import (
	"context"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator/migrations"
	"github.com/hookdeck/outpost/internal/migrator/migratorredis"
	"github.com/hookdeck/outpost/internal/redis"
	"go.uber.org/zap"
)

// runRedisMigrations handles Redis schema migrations with retry logic for lock conflicts.
//
// This mirrors the SQL migration behavior in migration.go:
// - Acquires a distributed lock before running migrations
// - Retries on lock conflicts (another instance is migrating)
// - Marks all migrations as applied on fresh installations
// - Only runs migrations that are marked as AutoRunnable
//
// deploymentID is optional - pass empty string for single-tenant deployments.
func runRedisMigrations(ctx context.Context, redisClient redis.Cmdable, logger *logging.Logger, deploymentID string) error {
	const (
		maxRetries = 3
		retryDelay = 5 * time.Second
	)

	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := executeRedisMigrations(ctx, redisClient, logger, deploymentID)
		if err == nil {
			return nil
		}

		isLockError := isRedisLockError(err)
		lastErr = err

		if !isLockError {
			logger.Error("redis migration failed", zap.Error(err))
			return err
		}

		// Lock error - retry if we have attempts remaining
		if attempt < maxRetries {
			logger.Warn("redis migration lock conflict, retrying",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Duration("retry_delay", retryDelay),
				zap.Error(err))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
			}
		} else {
			logger.Error("redis migration failed after retries",
				zap.Int("attempts", maxRetries),
				zap.Error(err))
		}
	}

	return lastErr
}

// executeRedisMigrations creates the runner and executes migrations
func executeRedisMigrations(ctx context.Context, redisClient redis.Cmdable, logger *logging.Logger, deploymentID string) error {
	client, ok := redisClient.(redis.Client)
	if !ok {
		// Wrap Cmdable to implement Client interface
		client = &redisClientAdapter{Cmdable: redisClient}
	}

	runner := migratorredis.NewRunner(client, logger)

	// Register all migrations from the central registry
	// Pass deployment ID to scope migrations to this deployment's keys
	for _, m := range migrations.AllRedisMigrationsWithLogging(client, logger, false, deploymentID) {
		runner.RegisterMigration(m)
	}

	// Run migrations (only auto-runnable ones will execute)
	return runner.Run(ctx)
}

// isRedisLockError checks if an error is related to Redis migration lock acquisition
func isRedisLockError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()
	lockIndicators := []string{
		"lock already held",
		"failed to acquire migration lock",
		"timeout waiting for redis initialization",
	}

	for _, indicator := range lockIndicators {
		if strings.Contains(errMsg, indicator) {
			return true
		}
	}

	return false
}

// redisClientAdapter wraps redis.Cmdable to implement redis.Client
type redisClientAdapter struct {
	redis.Cmdable
}

func (r *redisClientAdapter) Pipeline() redis.Pipeliner {
	return r.Cmdable.Pipeline()
}

func (r *redisClientAdapter) Close() error {
	// Cmdable doesn't have Close, this is a no-op
	return nil
}
