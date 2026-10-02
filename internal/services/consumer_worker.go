package services

import (
	"context"
	"errors"

	"github.com/hookdeck/outpost/internal/consumer"
	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/worker"
	"go.uber.org/zap"
)

// ConsumerWorker is a generic worker that wraps a message queue consumer.
// It handles subscription at runtime and consistent error handling for graceful shutdowns.
type ConsumerWorker struct {
	name        string
	subscribe   func(ctx context.Context, opts ...mqs.SubscribeOption) (mqs.Subscription, error)
	handler     consumer.MessageHandler
	concurrency int
	logger      *logging.Logger

	maxConsecutiveErrors int
}

// ConsumerWorkerOption configures a ConsumerWorker.
type ConsumerWorkerOption func(*ConsumerWorker)

// WithMaxConsecutiveErrors sets how many receive errors in a row end a run.
// Defaults to the consumer package default.
func WithMaxConsecutiveErrors(n int) ConsumerWorkerOption {
	return func(w *ConsumerWorker) {
		w.maxConsecutiveErrors = n
	}
}

// NewConsumerWorker creates a new generic consumer worker.
func NewConsumerWorker(
	name string,
	subscribe func(ctx context.Context, opts ...mqs.SubscribeOption) (mqs.Subscription, error),
	handler consumer.MessageHandler,
	concurrency int,
	logger *logging.Logger,
	opts ...ConsumerWorkerOption,
) worker.Worker {
	w := &ConsumerWorker{
		name:        name,
		subscribe:   subscribe,
		handler:     handler,
		concurrency: concurrency,
		logger:      logger,
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Name returns the worker name.
func (w *ConsumerWorker) Name() string {
	return w.name
}

// Run starts the consumer and blocks until context is cancelled or it fails.
func (w *ConsumerWorker) Run(ctx context.Context) error {
	logger := w.logger.Ctx(ctx)
	logger.Info("consumer worker starting", zap.String("name", w.name))

	subscription, err := w.subscribe(ctx, mqs.WithConcurrency(w.concurrency))
	if err != nil {
		logger.Error("error subscribing", zap.String("name", w.name), zap.Error(err))
		return err
	}

	worker.Ready(ctx)

	csmOpts := []consumer.Option{
		consumer.WithName(w.name),
		consumer.WithConcurrency(w.concurrency),
		consumer.WithLogger(w.logger),
	}
	if w.maxConsecutiveErrors > 0 {
		csmOpts = append(csmOpts, consumer.WithMaxConsecutiveErrors(w.maxConsecutiveErrors))
	}
	csm := consumer.New(subscription, w.handler, csmOpts...)

	if err := csm.Run(ctx); err != nil {
		// Only report as failure if it's not a graceful shutdown
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.Error("error running consumer", zap.String("name", w.name), zap.Error(err))
			return err
		}
	}

	return nil
}
