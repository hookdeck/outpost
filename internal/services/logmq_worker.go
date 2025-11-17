package services

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/logmq"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// LogMQWorker wraps a LogMQ consumer as a worker.
type LogMQWorker struct {
	logMQ       *logmq.LogMQ
	handler     consumer.MessageHandler
	concurrency int
	logger      *logging.Logger
}

// NewLogMQWorker creates a new LogMQ consumer worker.
func NewLogMQWorker(
	logMQ *logmq.LogMQ,
	handler consumer.MessageHandler,
	concurrency int,
	logger *logging.Logger,
) worker.Worker {
	return &LogMQWorker{
		logMQ:       logMQ,
		handler:     handler,
		concurrency: concurrency,
		logger:      logger,
	}
}

// Name returns the worker name.
func (w *LogMQWorker) Name() string {
	return "logmq-consumer"
}

// Run starts the LogMQ consumer and blocks until context is cancelled or it fails.
func (w *LogMQWorker) Run(ctx context.Context) error {
	logger := w.logger.Ctx(ctx)
	logger.Info("logmq consumer running")

	subscription, err := w.logMQ.Subscribe(ctx)
	if err != nil {
		logger.Error("error subscribing to logmq", zap.Error(err))
		return err
	}

	csm := consumer.New(subscription, w.handler,
		consumer.WithName("logmq"),
		consumer.WithConcurrency(w.concurrency),
	)

	if err := csm.Run(ctx); err != nil {
		// Only report as failure if it's not a graceful shutdown
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.Error("error running logmq consumer", zap.Error(err))
			return err
		}
	}

	return nil
}
