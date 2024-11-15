package consumer

import (
	"context"
	"log"

	"github.com/hookdeck/outpost/internal/mqs"
	"go.opentelemetry.io/otel"
)

type Consumer interface {
	Run(context.Context) error
}

type MessageHandler interface {
	Handle(context.Context, *mqs.Message) error
}

type consumerImplOptions struct {
	name        string
	concurrency int
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

func New(subscription mqs.Subscription, handler MessageHandler, opts ...func(*consumerImplOptions)) Consumer {
	options := &consumerImplOptions{
		name:        "",
		concurrency: 1,
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

	tracerProvider := otel.GetTracerProvider()
	tracer := tracerProvider.Tracer("github.com/hookdeck/outpost/internal/consumer")

	var subscriptionErr error

	sem := make(chan struct{}, c.concurrency)
recvLoop:
	for {
		msg, err := c.subscription.Receive(ctx)
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

			err = c.handler.Handle(handlerCtx, msg)
			// TODO: error handling?
			if err != nil {
				span.RecordError(err)
				log.Printf("consumer handler error: %v", err)
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
