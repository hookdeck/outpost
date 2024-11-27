package app

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/infra"
	"github.com/hookdeck/outpost/internal/otel"
	"github.com/hookdeck/outpost/internal/services/api"
	"github.com/hookdeck/outpost/internal/services/delivery"
	"github.com/hookdeck/outpost/internal/services/log"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
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
	zapLogger, err := zap.NewProduction()
	if err != nil {
		return err
	}
	defer zapLogger.Sync()
	logger := otelzap.New(zapLogger,
		otelzap.WithMinLevel(zap.InfoLevel), // TODO: allow configuration
	)

	chDB, err := clickhouse.New(cfg.ClickHouse)
	if err != nil {
		return err
	}
	defer chDB.Close()
	if err := clickhouse.RunMigration_Temporary(mainContext, chDB); err != nil {
		return err
	}

	if err := infra.Declare(mainContext, infra.Config{
		DeliveryMQ: cfg.DeliveryQueueConfig,
		LogMQ:      cfg.LogQueueConfig,
	}); err != nil {
		return err
	}

	// Set up cancellation context and waitgroup
	ctx, cancel := context.WithCancel(mainContext)

	// Set up OpenTelemetry.
	if cfg.OpenTelemetry != nil {
		otelShutdown, err := otel.SetupOTelSDK(ctx, cfg.OpenTelemetry)
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

	<-termChan // Blocks here until interrupted

	// Handle shutdown
	logger.Ctx(ctx).Info("*********************************\nShutdown signal received\n*********************************")
	cancel()  // Signal cancellation to context.Context
	wg.Wait() // Block here until all workers are done

	return nil
}

type Service interface {
	Run(ctx context.Context) error
}

func constructServices(
	ctx context.Context,
	cfg *config.Config,
	wg *sync.WaitGroup,
	logger *otelzap.Logger,
) ([]Service, error) {
	services := []Service{}

	if cfg.Service == config.ServiceTypeAPI || cfg.Service == config.ServiceTypeSingular {
		service, err := api.NewService(ctx, wg, cfg, logger)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	if cfg.Service == config.ServiceTypeDelivery || cfg.Service == config.ServiceTypeSingular {
		service, err := delivery.NewService(ctx, wg, cfg, logger, nil)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}
	if cfg.Service == config.ServiceTypeLog || cfg.Service == config.ServiceTypeSingular {
		service, err := log.NewService(ctx, wg, cfg, logger, nil)
		if err != nil {
			return nil, err
		}
		services = append(services, service)
	}

	return services, nil
}
