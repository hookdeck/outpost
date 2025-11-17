package app

import (
	"context"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"go.uber.org/zap"
)

// runMigration handles database schema migrations with retry logic for lock conflicts.
//
// MIGRATION LOCK BEHAVIOR:
// - Database locks are only acquired when migrations need to be performed
// - When multiple nodes start simultaneously and migrations are pending:
//  1. One node acquires the lock and performs migrations (ideally < 5 seconds)
//  2. Other nodes fail with lock errors ("try lock failed", "can't acquire lock")
//  3. Failed nodes wait 5 seconds and retry
//  4. On retry, migrations are complete and nodes proceed successfully
//
// RETRY STRATEGY:
// - Max 3 attempts with 5-second delays between retries
// - 5 seconds is sufficient because most migrations complete quickly
// - If no migrations are needed (common case), all nodes proceed immediately without lock contention
func runMigration(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	const (
		maxRetries = 3
		retryDelay = 5 * time.Second
	)

	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		migrator, err := migrator.New(cfg.ToMigratorOpts())
		if err != nil {
			return err
		}

		version, versionJumped, err := migrator.Up(ctx, -1)

		// Always close the migrator after each attempt
		sourceErr, dbErr := migrator.Close(ctx)
		if sourceErr != nil {
			logger.Error("failed to close migrator source", zap.Error(sourceErr))
		}
		if dbErr != nil {
			logger.Error("failed to close migrator database connection", zap.Error(dbErr))
		}

		if err == nil {
			// Migration succeeded
			if versionJumped > 0 {
				logger.Info("migrations applied",
					zap.Int("version", version),
					zap.Int("version_applied", versionJumped))
			} else {
				logger.Info("no migrations applied", zap.Int("version", version))
			}
			return nil
		}

		// Check if this is a lock-related error
		// Lock errors can manifest as:
		// - "can't acquire lock" (database.ErrLocked)
		// - "try lock failed" (postgres advisory lock failure)
		// - "pg_advisory_lock" (postgres lock function errors)
		isLockError := isLockRelatedError(err)
		lastErr = err

		if !isLockError {
			// Not a lock error, fail immediately
			logger.Error("migration failed", zap.Error(err))
			return err
		}

		// Lock error - retry if we have attempts remaining
		if attempt < maxRetries {
			logger.Warn("migration lock conflict, retrying",
				zap.Int("attempt", attempt),
				zap.Int("max_retries", maxRetries),
				zap.Duration("retry_delay", retryDelay),
				zap.Error(err))

			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retryDelay):
				// Continue to next attempt
			}
		} else {
			// Exhausted all retries
			logger.Error("migration failed after retries",
				zap.Int("attempts", maxRetries),
				zap.Error(err))
		}
	}

	return lastErr
}

// isLockRelatedError checks if an error is related to database migration lock acquisition.
// This includes errors from golang-migrate's locking mechanism.
func isLockRelatedError(err error) bool {
	if err == nil {
		return false
	}

	errMsg := err.Error()

	// Check for lock-related error messages from golang-migrate:
	// 1. "can't acquire lock" - database.ErrLocked from golang-migrate/migrate/v4/database
	// 2. "try lock failed" - returned by postgres driver when pg_advisory_lock() fails
	//    See: https://github.com/golang-migrate/migrate/blob/master/database/postgres/postgres.go
	lockIndicators := []string{
		"can't acquire lock",
		"try lock failed",
	}

	for _, indicator := range lockIndicators {
		if strings.Contains(errMsg, indicator) {
			return true
		}
	}

	return false
}
