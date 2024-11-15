package idempotence

import (
	"context"
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/redis"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
)

const (
	DefaultTimeout       = 5 * time.Second // 5 seconds
	DefaultSuccessfulTTL = 24 * time.Hour  // 24 hours
	StatusProcessing     = "processing"
	StatusProcessed      = "processed"
)

var ErrConflict = errors.New("conflict")

type Idempotence interface {
	Exec(ctx context.Context, key string, exec func(context.Context) error) error
}

type IdempotenceImpl struct {
	redisClient *redis.Client
	options     IdempotenceImplOptions
	tracer      trace.Tracer
}

type IdempotenceImplOptions struct {
	Timeout       time.Duration
	SuccessfulTTL time.Duration
}

func WithTimeout(timeout time.Duration) func(opts *IdempotenceImplOptions) {
	return func(opts *IdempotenceImplOptions) {
		opts.Timeout = timeout
	}
}

func WithSuccessfulTTL(successfulTTL time.Duration) func(opts *IdempotenceImplOptions) {
	return func(opts *IdempotenceImplOptions) {
		opts.SuccessfulTTL = successfulTTL
	}
}

func New(redisClient *redis.Client, opts ...func(opts *IdempotenceImplOptions)) Idempotence {
	options := &IdempotenceImplOptions{
		Timeout:       DefaultTimeout,
		SuccessfulTTL: DefaultSuccessfulTTL,
	}

	for _, opt := range opts {
		opt(options)
	}

	return &IdempotenceImpl{
		redisClient: redisClient,
		options:     *options,
		tracer:      otel.GetTracerProvider().Tracer("github.com/hookdeck/outpost/internal/idempotence"),
	}
}

var _ Idempotence = (*IdempotenceImpl)(nil)

func (i *IdempotenceImpl) Exec(ctx context.Context, key string, exec func(context.Context) error) error {
	isIdempotent, err := i.checkIdempotency(ctx, key)
	if err != nil {
		return err
	}
	if !isIdempotent {
		processingStatus, err := i.getIdempotencyStatus(ctx, key)
		if err != nil {
			// TODO: Question:
			// What if err == redis.Nil here? It happens
			// when the key is removed in between the initial SetNX and the Get here.
			// It also means that the previous consumer has err-ed out and removed the key.
			// Should we return "conflict" to nack the message so it triggers a retry later?
			// Also, should we account for this given the likelihood of it happening is very small?

			return err
		}
		if processingStatus == StatusProcessed {
			return nil
		}
		if processingStatus == StatusProcessing {
			time.Sleep(i.options.Timeout)
			status, err := i.getIdempotencyStatus(ctx, key)
			if err != nil {
				if err == redis.Nil {
					// The previous consumer has err-ed and removed the processing key. We should also err
					// so that it can be retried later.
					return ErrConflict
				}
				return err
			}
			if status == StatusProcessed {
				return nil
			}
			return ErrConflict
		}
		return errors.New("unknown idempotency status")
	}

	execCtx, span := i.tracer.Start(ctx, "Idempotence.Exec")
	err = exec(execCtx)
	if err != nil {
		clearErr := i.clearIdempotency(ctx, key)
		if clearErr != nil {
			finalErr := errors.Join(err, clearErr)
			span.RecordError(finalErr)
			span.End()
			return finalErr
		}
		span.RecordError(err)
		span.End()
		return err
	} else {
		span.End()
	}

	err = i.markProcessedIdempotency(ctx, key)
	if err != nil {
		// TODO: Question: how to properly handle this error?
		return err
	}

	return nil
}

func (i *IdempotenceImpl) checkIdempotency(ctx context.Context, idempotencyKey string) (bool, error) {
	idempotentValue, err := i.redisClient.SetNX(ctx, idempotencyKey, StatusProcessing, i.options.Timeout).Result()
	if err != nil {
		return false, err
	}
	return idempotentValue, nil
}

func (i *IdempotenceImpl) getIdempotencyStatus(ctx context.Context, idempotencyKey string) (string, error) {
	return i.redisClient.Get(ctx, idempotencyKey).Result()
}

func (i *IdempotenceImpl) markProcessedIdempotency(ctx context.Context, idempotencyKey string) error {
	return i.redisClient.Set(ctx, idempotencyKey, StatusProcessed, i.options.SuccessfulTTL).Err()
}

func (i *IdempotenceImpl) clearIdempotency(ctx context.Context, idempotencyKey string) error {
	return i.redisClient.Del(ctx, idempotencyKey).Err()
}
