package app

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/otel"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/telemetry"
	"go.uber.org/zap"
)

type App struct {
	config *config.Config
}

func New(cfg *config.Config) *App {
	return &App{
		config: cfg,
	}
}

func (a *App) Run(ctx context.Context) error {
	return run(ctx, a.config)
}

func run(mainContext context.Context, cfg *config.Config) error {
	logger, err := logging.NewLogger(
		logging.WithLogLevel(cfg.LogLevel),
		logging.WithAuditLog(cfg.AuditLog),
	)
	if err != nil {
		return err
	}
	defer logger.Sync()

	logFields := []zap.Field{
		zap.String("config_path", cfg.ConfigFilePath()),
		zap.String("service", cfg.MustGetService().String()),
	}
	if cfg.DeploymentID != "" {
		logFields = append(logFields, zap.String("deployment_id", cfg.DeploymentID))
	}
	logger.Info("starting outpost", logFields...)

	// Initialize ID generators
	logger.Debug("configuring ID generators",
		zap.String("type", cfg.IDGen.Type),
		zap.String("event_prefix", cfg.IDGen.EventPrefix),
		zap.String("destination_prefix", cfg.IDGen.DestinationPrefix),
		zap.String("delivery_prefix", cfg.IDGen.DeliveryPrefix),
		zap.String("delivery_event_prefix", cfg.IDGen.DeliveryEventPrefix))
	if err := idgen.Configure(idgen.IDGenConfig{
		Type:                cfg.IDGen.Type,
		EventPrefix:         cfg.IDGen.EventPrefix,
		DestinationPrefix:   cfg.IDGen.DestinationPrefix,
		DeliveryPrefix:      cfg.IDGen.DeliveryPrefix,
		DeliveryEventPrefix: cfg.IDGen.DeliveryEventPrefix,
	}); err != nil {
		logger.Error("failed to configure ID generators", zap.Error(err))
		return err
	}

	if err := runMigration(mainContext, cfg, logger); err != nil {
		return err
	}

	logger.Debug("initializing Redis client for infrastructure")
	// Create Redis client for infrastructure components
	redisClient, err := redis.New(mainContext, cfg.Redis.ToConfig())
	if err != nil {
		logger.Error("Redis client initialization failed", zap.Error(err))
		return err
	}

	logger.Debug("creating Outpost infrastructure")
	outpostInfra := infra.NewInfra(infra.Config{
		DeliveryMQ: cfg.MQs.ToInfraConfig("deliverymq"),
		LogMQ:      cfg.MQs.ToInfraConfig("logmq"),
	}, redisClient)

	// Only declare infrastructure if should_manage is true (default)
	shouldManage := cfg.MQs.ShouldManage == nil || *cfg.MQs.ShouldManage
	if shouldManage {
		logger.Debug("infrastructure management enabled, declaring infrastructure")
		if err := outpostInfra.Declare(mainContext); err != nil {
			logger.Error("infrastructure declaration failed", zap.Error(err))
			return err
		}
	} else {
		logger.Info("infrastructure management disabled, verifying infrastructure exists")
		if err := outpostInfra.Verify(mainContext); err != nil {
			logger.Error("infrastructure verification failed", zap.Error(err))
			return err
		}
		logger.Info("infrastructure verification successful")
	}

	installationID, err := getInstallation(mainContext, redisClient, cfg.Telemetry.ToTelemetryConfig())
	if err != nil {
		return err
	}

	telemetry := telemetry.New(logger, cfg.Telemetry.ToTelemetryConfig(), installationID)
	telemetry.Init(mainContext)
	telemetry.ApplicationStarted(mainContext, cfg.ToTelemetryApplicationInfo())

	// Set up cancellation context
	ctx, cancel := context.WithCancel(mainContext)
	defer cancel()

	// Set up OpenTelemetry.
	if cfg.OpenTelemetry.ToConfig() != nil {
		otelShutdown, err := otel.SetupOTelSDK(ctx, cfg.OpenTelemetry.ToConfig())
		if err != nil {
			return err
		}
		// Handle shutdown properly so nothing leaks.
		defer func() {
			err = errors.Join(err, otelShutdown(context.Background()))
		}()
	}

	// Build services using ServiceBuilder
	logger.Debug("building services")
	builder := services.NewServiceBuilder(ctx, cfg, logger, telemetry)

	supervisor, err := builder.BuildWorkers()
	if err != nil {
		logger.Error("failed to build workers", zap.Error(err))
		return err
	}

	// Handle sigterm and await termChan signal
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	// Run workers in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- supervisor.Run(ctx)
	}()

	// Wait for either termination signal or worker failure
	var exitErr error
	select {
	case <-termChan:
		logger.Info("shutdown signal received")
		cancel() // Cancel context to trigger graceful shutdown
		err := <-errChan
		// context.Canceled is expected during graceful shutdown
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("error during graceful shutdown", zap.Error(err))
			exitErr = err
		}
	case err := <-errChan:
		// Workers exited unexpectedly
		if err != nil {
			logger.Error("workers exited unexpectedly", zap.Error(err))
			exitErr = err
		}
	}

	telemetry.Flush()

	// Run cleanup functions
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	builder.Cleanup(shutdownCtx)

	logger.Info("outpost shutdown complete")

	return exitErr
}

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
