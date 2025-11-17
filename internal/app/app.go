package app

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/idgen"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/otel"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/telemetry"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

type App struct {
	config *config.Config
	logger *logging.Logger

	// Runtime dependencies
	redisClient    redis.Cmdable
	telemetry      telemetry.Telemetry
	builder        *services.ServiceBuilder
	supervisor     *worker.WorkerSupervisor
	otelShutdown   func(context.Context) error
	installationID string
}

func New(cfg *config.Config) *App {
	return &App{
		config: cfg,
	}
}

func (a *App) Run(ctx context.Context) error {
	if err := a.PreRun(ctx); err != nil {
		return err
	}
	defer a.PostRun(ctx)

	return a.run(ctx)
}

// PreRun initializes all dependencies before starting the application
func (a *App) PreRun(ctx context.Context) error {
	if err := a.setupLogger(); err != nil {
		return err
	}
	defer func() {
		if r := recover(); r != nil {
			a.logger.Error("panic during PreRun", zap.Any("panic", r))
		}
	}()

	a.logger.Info("starting outpost",
		zap.String("config_path", a.config.ConfigFilePath()),
		zap.String("service", a.config.MustGetService().String()))

	if a.config.DeploymentID != "" {
		a.logger.Info("deployment configured", zap.String("deployment_id", a.config.DeploymentID))
	}

	if err := a.configureIDGenerators(); err != nil {
		return err
	}

	if err := a.runMigrations(ctx); err != nil {
		return err
	}

	if err := a.initializeRedis(ctx); err != nil {
		return err
	}

	if err := a.initializeInfrastructure(ctx); err != nil {
		return err
	}

	if err := a.initializeTelemetry(ctx); err != nil {
		return err
	}

	if err := a.setupOpenTelemetry(ctx); err != nil {
		return err
	}

	if err := a.buildServices(ctx); err != nil {
		return err
	}

	return nil
}

// PostRun handles cleanup after application exits
func (a *App) PostRun(ctx context.Context) {
	if a.telemetry != nil {
		a.telemetry.Flush()
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if a.builder != nil {
		a.builder.Cleanup(shutdownCtx)
	}

	if a.otelShutdown != nil {
		if err := a.otelShutdown(context.Background()); err != nil {
			a.logger.Error("OpenTelemetry shutdown error", zap.Error(err))
		}
	}

	if a.logger != nil {
		a.logger.Info("outpost shutdown complete")
		a.logger.Sync()
	}
}

func (a *App) run(ctx context.Context) error {
	// Set up cancellation context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle sigterm and await termChan signal
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	// Run workers in goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- a.supervisor.Run(ctx)
	}()

	// Wait for either termination signal or worker failure
	var exitErr error
	select {
	case <-termChan:
		a.logger.Info("shutdown signal received")
		cancel() // Cancel context to trigger graceful shutdown
		err := <-errChan
		// context.Canceled is expected during graceful shutdown
		if err != nil && !errors.Is(err, context.Canceled) {
			a.logger.Error("error during graceful shutdown", zap.Error(err))
			exitErr = err
		}
	case err := <-errChan:
		// Workers exited unexpectedly
		if err != nil {
			a.logger.Error("workers exited unexpectedly", zap.Error(err))
			exitErr = err
		}
	}

	return exitErr
}

func (a *App) setupLogger() error {
	logger, err := logging.NewLogger(
		logging.WithLogLevel(a.config.LogLevel),
		logging.WithAuditLog(a.config.AuditLog),
	)
	if err != nil {
		return err
	}
	a.logger = logger
	return nil
}

func (a *App) configureIDGenerators() error {
	a.logger.Debug("configuring ID generators",
		zap.String("type", a.config.IDGen.Type),
		zap.String("event_prefix", a.config.IDGen.EventPrefix),
		zap.String("destination_prefix", a.config.IDGen.DestinationPrefix),
		zap.String("delivery_prefix", a.config.IDGen.DeliveryPrefix),
		zap.String("delivery_event_prefix", a.config.IDGen.DeliveryEventPrefix))

	if err := idgen.Configure(idgen.IDGenConfig{
		Type:                a.config.IDGen.Type,
		EventPrefix:         a.config.IDGen.EventPrefix,
		DestinationPrefix:   a.config.IDGen.DestinationPrefix,
		DeliveryPrefix:      a.config.IDGen.DeliveryPrefix,
		DeliveryEventPrefix: a.config.IDGen.DeliveryEventPrefix,
	}); err != nil {
		a.logger.Error("failed to configure ID generators", zap.Error(err))
		return err
	}
	return nil
}

func (a *App) runMigrations(ctx context.Context) error {
	return runMigration(ctx, a.config, a.logger)
}

func (a *App) initializeRedis(ctx context.Context) error {
	a.logger.Debug("initializing Redis client for infrastructure")
	redisClient, err := redis.New(ctx, a.config.Redis.ToConfig())
	if err != nil {
		a.logger.Error("Redis client initialization failed", zap.Error(err))
		return err
	}
	a.redisClient = redisClient
	return nil
}

func (a *App) initializeInfrastructure(ctx context.Context) error {
	a.logger.Debug("initializing infrastructure")
	if err := infra.Init(ctx, infra.Config{
		DeliveryMQ:    a.config.MQs.ToInfraConfig("deliverymq"),
		LogMQ:         a.config.MQs.ToInfraConfig("logmq"),
		AutoProvision: a.config.MQs.AutoProvision,
	}, a.redisClient); err != nil {
		a.logger.Error("infrastructure initialization failed", zap.Error(err))
		return err
	}
	return nil
}

func (a *App) initializeTelemetry(ctx context.Context) error {
	installationID, err := getInstallation(ctx, a.redisClient, a.config.Telemetry.ToTelemetryConfig())
	if err != nil {
		return err
	}
	a.installationID = installationID

	a.telemetry = telemetry.New(a.logger, a.config.Telemetry.ToTelemetryConfig(), installationID)
	a.telemetry.Init(ctx)
	a.telemetry.ApplicationStarted(ctx, a.config.ToTelemetryApplicationInfo())
	return nil
}

func (a *App) setupOpenTelemetry(ctx context.Context) error {
	if a.config.OpenTelemetry.ToConfig() != nil {
		otelShutdown, err := otel.SetupOTelSDK(ctx, a.config.OpenTelemetry.ToConfig())
		if err != nil {
			return err
		}
		a.otelShutdown = otelShutdown
	}
	return nil
}

func (a *App) buildServices(ctx context.Context) error {
	a.logger.Debug("building services")
	builder := services.NewServiceBuilder(ctx, a.config, a.logger, a.telemetry)

	supervisor, err := builder.BuildWorkers()
	if err != nil {
		a.logger.Error("failed to build workers", zap.Error(err))
		return err
	}

	a.builder = builder
	a.supervisor = supervisor
	return nil
}
