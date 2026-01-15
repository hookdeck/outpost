package services

import (
	"context"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/scheduler"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// RetryMQWorker wraps a retry scheduler as a worker.
type RetryMQWorker struct {
	scheduler scheduler.Scheduler
	logger    *logging.Logger
}

// NewRetryMQWorker creates a new retry scheduler worker.
func NewRetryMQWorker(scheduler scheduler.Scheduler, logger *logging.Logger) worker.Worker {
	return &RetryMQWorker{
		scheduler: scheduler,
		logger:    logger,
	}
}

// Name returns the worker name.
func (w *RetryMQWorker) Name() string {
	return "retrymq-consumer"
}

// Run starts the retry scheduler monitor and blocks until context is cancelled or it fails.
func (w *RetryMQWorker) Run(ctx context.Context) error {
	logger := w.logger.Ctx(ctx)
	logger.Info("retry scheduler monitor running")

	if err := w.scheduler.Monitor(ctx); err != nil {
		logger.Error("retry scheduler monitor error", zap.Error(err))
		return err
	}

	return nil
}
