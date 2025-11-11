package services

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// DeliveryMQWorker wraps a DeliveryMQ consumer as a worker.
type DeliveryMQWorker struct {
	deliveryMQ  *deliverymq.DeliveryMQ
	handler     consumer.MessageHandler
	concurrency int
	logger      *logging.Logger
}

// NewDeliveryMQWorker creates a new DeliveryMQ consumer worker.
func NewDeliveryMQWorker(
	deliveryMQ *deliverymq.DeliveryMQ,
	handler consumer.MessageHandler,
	concurrency int,
	logger *logging.Logger,
) worker.Worker {
	return &DeliveryMQWorker{
		deliveryMQ:  deliveryMQ,
		handler:     handler,
		concurrency: concurrency,
		logger:      logger,
	}
}

// Name returns the worker name.
func (w *DeliveryMQWorker) Name() string {
	return "deliverymq-consumer"
}

// Run starts the DeliveryMQ consumer and blocks until context is cancelled or it fails.
func (w *DeliveryMQWorker) Run(ctx context.Context) error {
	logger := w.logger.Ctx(ctx)
	logger.Info("deliverymq consumer running")

	subscription, err := w.deliveryMQ.Subscribe(ctx)
	if err != nil {
		logger.Error("error subscribing to deliverymq", zap.Error(err))
		return err
	}

	csm := consumer.New(subscription, w.handler,
		consumer.WithName("deliverymq"),
		consumer.WithConcurrency(w.concurrency),
	)

	if err := csm.Run(ctx); !errors.Is(err, ctx.Err()) {
		logger.Error("error running deliverymq consumer", zap.Error(err))
		return err
	}

	return nil
}
