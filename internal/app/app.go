package app

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/migrator"
	"github.com/hookdeck/outpost/internal/otel"
	"github.com/hookdeck/outpost/internal/services/api"
	"github.com/hookdeck/outpost/internal/services/delivery"
	"github.com/hookdeck/outpost/internal/services/log"
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

	logger.Info("starting outpost",
		zap.String("config_path", cfg.ConfigFilePath()),
		zap.String("service", cfg.MustGetService().String()),
	)

	if err := runMigration(mainContext, cfg, logger); err != nil {
		return err
	}

	if err := infra.Declare(mainContext, infra.Config{
		DeliveryMQ: cfg.MQs.GetDeliveryQueueConfig(),
		LogMQ:      cfg.MQs.GetLogQueueConfig(),
	}); err != nil {
		return err
	}

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
	services, err := constructServices(
		ctx,
		cfg,
		wg,
		logger,
	)
	if err != nil {
		cancel()
		return err
	}

	// Start services
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
) ([]Service, error) {
	serviceType := cfg.MustGetService()
	services := []Service{}

	if serviceType == config.ServiceTypeAPI || serviceType == config.ServiceTypeAll {
		service, err := api.NewService(ctx, wg, cfg, logger)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	if serviceType == config.ServiceTypeDelivery || serviceType == config.ServiceTypeAll {
		service, err := delivery.NewService(ctx, wg, cfg, logger, nil)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	if serviceType == config.ServiceTypeLog || serviceType == config.ServiceTypeAll {
		service, err := log.NewService(ctx, wg, cfg, logger, nil)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}

	return services, nil
}

func runMigration(ctx context.Context, cfg *config.Config, logger *logging.Logger) error {
	migrator, err := migrator.New(cfg.ToMigratorOpts())
	if err != nil {
		return err
	}

	defer func() {
		sourceErr, dbErr := migrator.Close(ctx)
		if sourceErr != nil {
			logger.Error("failed to close migrator", zap.Error(sourceErr))
		}
		if dbErr != nil {
			logger.Error("failed to close migrator", zap.Error(dbErr))
		}
	}()

	version, versionJumped, err := migrator.Up(ctx, -1)
	if err != nil {
		return err
	}
	if versionJumped > 0 {
		logger.Info("migrations applied",
			zap.Int("version", version),
			zap.Int("version_applied", versionJumped))
	} else {
		logger.Info("no migrations applied", zap.Int("version", version))
	}

	return nil
}
