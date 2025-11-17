package services

import (
	"context"
	"net/http"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// HTTPServerWorker wraps an HTTP server as a worker.
type HTTPServerWorker struct {
	server *http.Server
	logger *logging.Logger
}

// NewHTTPServerWorker creates a new HTTP server worker.
func NewHTTPServerWorker(server *http.Server, logger *logging.Logger) worker.Worker {
	return &HTTPServerWorker{
		server: server,
		logger: logger,
	}
}

// Name returns the worker name.
func (w *HTTPServerWorker) Name() string {
	return "http-server"
}

// Run starts the HTTP server and blocks until context is cancelled or server fails.
func (w *HTTPServerWorker) Run(ctx context.Context) error {
	logger := w.logger.Ctx(ctx)
	logger.Info("http server listening", zap.String("addr", w.server.Addr))

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := w.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	// Wait for context cancellation or server error
	select {
	case <-ctx.Done():
		// Graceful shutdown
		logger.Info("shutting down http server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := w.server.Shutdown(shutdownCtx); err != nil {
			logger.Error("error shutting down http server", zap.Error(err))
			return err
		}
		logger.Info("http server shut down")
		return nil

	case err := <-errChan:
		logger.Error("http server error", zap.Error(err))
		return err
	}
}
