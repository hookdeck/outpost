package app

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/otel"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/services/api"
	"github.com/hookdeck/outpost/internal/services/delivery"
	"github.com/hookdeck/outpost/internal/services/log"
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
		zap.String("delivery_prefix", cfg.IDGen.DeliveryPrefix))
	if err := idgen.Configure(idgen.IDGenConfig{
		Type:              cfg.IDGen.Type,
		EventPrefix:       cfg.IDGen.EventPrefix,
		DestinationPrefix: cfg.IDGen.DestinationPrefix,
		DeliveryPrefix:    cfg.IDGen.DeliveryPrefix,
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
	if err := outpostInfra.Declare(mainContext); err != nil {
		logger.Error("infrastructure declaration failed", zap.Error(err))
		return err
	}

	installationID, err := getInstallation(mainContext, redisClient, cfg.Telemetry.ToTelemetryConfig())
	if err != nil {
		return err
	}

	telemetry := telemetry.New(logger, cfg.Telemetry.ToTelemetryConfig(), installationID)
	telemetry.Init(mainContext)
	telemetry.ApplicationStarted(mainContext, cfg.ToTelemetryApplicationInfo())

	// Set up cancellation context and waitgroup
	ctx, cancel := context.WithCancel(mainContext)

	// Set up OpenTelemetry.
	if cfg.OpenTelemetry.ToConfig() != nil {
		otelShutdown, err := otel.SetupOTelSDK(ctx, cfg.OpenTelemetry.ToConfig())
		if err != nil {
			cancel()
			return err
		}
		// Handle shutdown properly so nothing leaks.
		defer func() {
			err = errors.Join(err, otelShutdown(context.Background()))
		}()
	}

	// Initialize waitgroup
	// Once all services are done, we can exit.
	// Each service will wait for the context to be cancelled before shutting down.
	wg := &sync.WaitGroup{}

	// Construct services based on config
	logger.Debug("constructing services")
	services, err := constructServices(
		ctx,
		cfg,
		wg,
		logger,
		telemetry,
	)
	if err != nil {
		logger.Error("service construction failed", zap.Error(err))
		cancel()
		return err
	}

	// Start services
	logger.Info("starting services", zap.Int("count", len(services)))
	for _, service := range services {
		go service.Run(ctx)
	}

	// Handle sigterm and await termChan signal
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either context cancellation or termination signal
	select {
	case <-termChan:
		logger.Ctx(ctx).Info("shutdown signal received")
	case <-ctx.Done():
		logger.Ctx(ctx).Info("context cancelled")
	}

	telemetry.Flush()

	// Handle shutdown
	cancel()  // Signal cancellation to context.Context
	wg.Wait() // Block here until all workers are done

	logger.Ctx(ctx).Info("outpost shutdown complete")

	return nil
}

type Service interface {
	Run(ctx context.Context) error
}

func constructServices(
	ctx context.Context,
	cfg *config.Config,
	wg *sync.WaitGroup,
	logger *logging.Logger,
	telemetry telemetry.Telemetry,
) ([]Service, error) {
	serviceType := cfg.MustGetService()
	services := []Service{}

	if serviceType == config.ServiceTypeAPI || serviceType == config.ServiceTypeAll {
		logger.Debug("creating API service")
		service, err := api.NewService(ctx, wg, cfg, logger, telemetry)
		if err != nil {
			logger.Error("API service creation failed", zap.Error(err))
			return nil, err
		}
		services = append(services, service)
	}
	if serviceType == config.ServiceTypeDelivery || serviceType == config.ServiceTypeAll {
		logger.Debug("creating delivery service")
		service, err := delivery.NewService(ctx, wg, cfg, logger, nil)
		if err != nil {
			logger.Error("delivery service creation failed", zap.Error(err))
			return nil, err
		}
		services = append(services, service)
	}
	if serviceType == config.ServiceTypeLog || serviceType == config.ServiceTypeAll {
		logger.Debug("creating log service")
		service, err := log.NewService(ctx, wg, cfg, logger, nil)
		if err != nil {
			logger.Error("log service creation failed", zap.Error(err))
			return nil, err
		}
		services = append(services, service)
	}

	return services, nil
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
