package consumer

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/mqs"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

// Error retry schedule (with 200ms initial backoff):
//
//	Error  Backoff    Cumulative
//	1      200ms      0.2s
//	2      400ms      0.6s
//	3      800ms      1.4s       ← 3 retries within ~1.5s
//	4      1.6s       3.0s
//	5      3.2s       6.2s
//	6      6.4s       12.6s
//	7      12.8s      25.4s
//	8      15s (cap)  40.4s
//	9      15s (cap)  55.4s
//	10     15s (cap)  70.4s      ← worker dies (~1 min total)
//
// Backoff formula: initialBackoff * 2^(attempt-1), capped at maxBackoff.
// After maxConsecutiveErrors the worker dies permanently (supervisor does
// not restart it), so these values must tolerate transient infra outages
// (e.g. brief MQ broker restarts, GCP OAuth/DNS blips) without killing the
// worker. ~1 min is sufficient for managed broker recovery from routine
// restarts or short network blips.
const (
	defaultMaxConsecutiveErrors = 10
	defaultInitialBackoff       = 200 * time.Millisecond
	defaultMaxBackoff           = 15 * time.Second
)

type Consumer interface {
	Run(context.Context) error
}

type MessageHandler interface {
	Handle(context.Context, *mqs.Message) error
}

type consumerImplOptions struct {
	name                 string
	concurrency          int
	logger               *logging.Logger
	maxConsecutiveErrors int
	initialBackoff       time.Duration
	maxBackoff           time.Duration
}

func WithName(name string) func(*consumerImplOptions) {
	return func(c *consumerImplOptions) {
		c.name = name
	}
}

func WithConcurrency(concurrency int) func(*consumerImplOptions) {
	return func(c *consumerImplOptions) {
		c.concurrency = concurrency
	}
}

func WithLogger(logger *logging.Logger) func(*consumerImplOptions) {
	return func(c *consumerImplOptions) {
		c.logger = logger
	}
}

func WithMaxConsecutiveErrors(n int) func(*consumerImplOptions) {
	return func(c *consumerImplOptions) {
		c.maxConsecutiveErrors = n
	}
}

func WithInitialBackoff(d time.Duration) func(*consumerImplOptions) {
	return func(c *consumerImplOptions) {
		c.initialBackoff = d
	}
}

func WithMaxBackoff(d time.Duration) func(*consumerImplOptions) {
	return func(c *consumerImplOptions) {
		c.maxBackoff = d
	}
}

func New(subscription mqs.Subscription, handler MessageHandler, opts ...func(*consumerImplOptions)) Consumer {
	options := &consumerImplOptions{
		name:                 "",
		concurrency:          1,
		maxConsecutiveErrors: defaultMaxConsecutiveErrors,
		initialBackoff:       defaultInitialBackoff,
		maxBackoff:           defaultMaxBackoff,
	}
	for _, opt := range opts {
		opt(options)
	}
	return &consumerImpl{
		subscription:        subscription,
		handler:             handler,
		consumerImplOptions: *options,
	}
}

type consumerImpl struct {
	consumerImplOptions
	subscription mqs.Subscription
	handler      MessageHandler
}

var _ Consumer = &consumerImpl{}

func (c *consumerImpl) Run(ctx context.Context) error {
	defer c.subscription.Shutdown(ctx)

	// If the subscription manages its own concurrency (e.g. GCP native SDK
	// with MaxOutstandingMessages), skip the consumer-side semaphore.
	if cs, ok := c.subscription.(mqs.ConcurrentSubscription); ok && cs.SupportsConcurrency() {
		return c.runConcurrent(ctx)
	}
	return c.runWithSemaphore(ctx)
}

// receiveWithRetry wraps subscription.Receive with exponential backoff on errors.
// Returns (nil, err) only after maxConsecutiveErrors consecutive failures.
func (c *consumerImpl) receiveWithRetry(ctx context.Context, consecutiveErrors *int) (*mqs.Message, error) {
	for {
		msg, err := c.subscription.Receive(ctx)
		if err == nil {
			*consecutiveErrors = 0
			return msg, nil
		}

		*consecutiveErrors++
		if *consecutiveErrors >= c.maxConsecutiveErrors {
			return nil, fmt.Errorf("max consecutive receive errors reached (%d): %w", c.maxConsecutiveErrors, err)
		}

		backoff := c.initialBackoff * time.Duration(1<<(*consecutiveErrors-1))
		if backoff > c.maxBackoff {
			backoff = c.maxBackoff
		}

		if c.logger != nil {
			c.logger.Ctx(ctx).Warn("consumer receive error, retrying",
				zap.String("name", c.name),
				zap.Error(err),
				zap.Int("attempt", *consecutiveErrors),
				zap.Duration("backoff", backoff))
		}

		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// runConcurrent is used when the subscription manages flow control internally.
// A WaitGroup tracks in-flight handlers for graceful shutdown.
func (c *consumerImpl) runConcurrent(ctx context.Context) error {
	tracer := otel.GetTracerProvider().Tracer("github.com/hookdeck/outpost/internal/consumer")

	var wg sync.WaitGroup
	var subscriptionErr error
	consecutiveErrors := 0

recvLoop:
	for {
		msg, err := c.receiveWithRetry(ctx, &consecutiveErrors)
		if err != nil {
			subscriptionErr = err
			break recvLoop
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			handlerCtx, span := tracer.Start(context.Background(), c.actionWithName("Consumer.Handle"))
			defer span.End()

			if err := c.handler.Handle(handlerCtx, msg); err != nil {
				span.RecordError(err)
				if c.logger != nil {
					c.logger.Ctx(handlerCtx).Error("consumer handler error", zap.String("name", c.name), zap.Error(err))
				}
			}
		}()
	}

	wg.Wait()
	return subscriptionErr
}

// runWithSemaphore limits concurrency via a channel-based semaphore.
func (c *consumerImpl) runWithSemaphore(ctx context.Context) error {
	tracer := otel.GetTracerProvider().Tracer("github.com/hookdeck/outpost/internal/consumer")

	var subscriptionErr error
	consecutiveErrors := 0

	sem := make(chan struct{}, c.concurrency)
recvLoop:
	for {
		msg, err := c.receiveWithRetry(ctx, &consecutiveErrors)
		if err != nil {
			subscriptionErr = err
			break recvLoop
		}

		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			break recvLoop
		}

		go func() {
			defer func() { <-sem }() // Release the semaphore.

			handlerCtx, span := tracer.Start(context.Background(), c.actionWithName("Consumer.Handle"))
			defer span.End()

			if err := c.handler.Handle(handlerCtx, msg); err != nil {
				span.RecordError(err)
				if c.logger != nil {
					c.logger.Ctx(handlerCtx).Error("consumer handler error", zap.String("name", c.name), zap.Error(err))
				}
			}
		}()
	}

	// We're no longer receiving messages. Wait to finish handling any
	// unacknowledged messages by totally acquiring the semaphore.
	for n := 0; n < c.concurrency; n++ {
		sem <- struct{}{}
	}

	return subscriptionErr
}

func (c *consumerImpl) actionWithName(action string) string {
	if c.name == "" {
		return action
	}
	return c.name + "." + action
}
