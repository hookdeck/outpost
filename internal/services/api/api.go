package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/models"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/uptrace/opentelemetry-go-extra/otelzap"
	"go.uber.org/zap"
)

type APIService struct {
	server *http.Server
	logger *otelzap.Logger
}

func NewService(ctx context.Context, wg *sync.WaitGroup, cfg *config.Config, logger *otelzap.Logger, ingestor *ingest.Ingestor) (*APIService, error) {
	wg.Add(1)

	redisClient, err := redis.New(ctx, cfg.Redis)
	if err != nil {
		return nil, err
	}

	router := NewRouter(
		RouterConfig{
			Hostname:  cfg.Hostname,
			APIKey:    cfg.APIKey,
			JWTSecret: cfg.JWTSecret,
		},
		logger,
		redisClient,
		models.NewTenantModel(),
		models.NewDestinationModel(),
		ingestor,
	)

	service := &APIService{}
	service.logger = logger
	service.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.Port),
		Handler: router,
	}

	go func() {
		defer wg.Done()
		<-ctx.Done()
		logger.Ctx(ctx).Info("shutting down api service")
		// make a new context for Shutdown
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := service.server.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintf(os.Stderr, "error shutting down http server: %s\n", err)
		}
		logger.Ctx(ctx).Info("http server shutted down")
	}()

	return service, nil
}

func (s *APIService) Run(ctx context.Context) error {
	s.logger.Ctx(ctx).Info("start service", zap.String("service", "api"))
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Ctx(ctx).Error(fmt.Sprintf("error listening and serving: %s\n", err), zap.Error(err))
		}
	}()
	return nil
}
