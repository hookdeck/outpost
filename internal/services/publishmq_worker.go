package services

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/publishmq"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// PublishMQWorker wraps a PublishMQ consumer as a worker.
type PublishMQWorker struct {
	publishMQ    *publishmq.PublishMQ
	eventHandler publishmq.EventHandler
	concurrency  int
	logger       *logging.Logger
}

// NewPublishMQWorker creates a new PublishMQ consumer worker.
func NewPublishMQWorker(
	publishMQ *publishmq.PublishMQ,
	eventHandler publishmq.EventHandler,
	concurrency int,
	logger *logging.Logger,
) worker.Worker {
	return &PublishMQWorker{
		publishMQ:    publishMQ,
		eventHandler: eventHandler,
		concurrency:  concurrency,
		logger:       logger,
	}
}

// Name returns the worker name.
func (w *PublishMQWorker) Name() string {
	return "publishmq-consumer"
}

// Run starts the PublishMQ consumer and blocks until context is cancelled or it fails.
func (w *PublishMQWorker) Run(ctx context.Context) error {
	logger := w.logger.Ctx(ctx)
	logger.Info("publishmq consumer running")

	subscription, err := w.publishMQ.Subscribe(ctx)
	if err != nil {
		logger.Error("error subscribing to publishmq", zap.Error(err))
		return err
	}

	messageHandler := publishmq.NewMessageHandler(w.eventHandler)
	csm := consumer.New(subscription, messageHandler,
		consumer.WithName("publishmq"),
		consumer.WithConcurrency(w.concurrency),
	)

	if err := csm.Run(ctx); !errors.Is(err, ctx.Err()) {
		logger.Error("error running publishmq consumer", zap.Error(err))
		return err
	}

	return nil
}
