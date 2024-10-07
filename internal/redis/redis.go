package redis

import (
	"context"
	"fmt"
	"sync"

	"github.com/redis/go-redis/extra/redisotel/v9"
	r "github.com/redis/go-redis/v9"
)

// Reexport go-redis's Nil constant for DX purposes.
const (
	Nil = r.Nil
)

type (
	Client             = r.Client
	Cmdable            = r.Cmdable
	MapStringStringCmd = r.MapStringStringCmd
	Pipeliner          = r.Pipeliner
	Tx                 = r.Tx
)

const (
	TxFailedErr = r.TxFailedErr
)

var (
	once                sync.Once
	client              *r.Client
	initializationError error
)

func New(ctx context.Context, config *RedisConfig) (*r.Client, error) {
	once.Do(func() {
		initializeClient(ctx, config)
		initializationError = instrumentOpenTelemetry()
	})
	return client, initializationError
}

func instrumentOpenTelemetry() error {
	if err := redisotel.InstrumentTracing(client); err != nil {
		return err
	}
	if err := redisotel.InstrumentMetrics(client); err != nil {
		return err
	}
	return nil
}

func initializeClient(_ context.Context, config *RedisConfig) {
	client = r.NewClient(&r.Options{
		Addr:     fmt.Sprintf("%s:%d", config.Host, config.Port),
		Password: config.Password,
		DB:       config.Database,
	})
}
