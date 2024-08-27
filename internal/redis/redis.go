package redis

import (
	"context"
	"fmt"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/redis/go-redis/extra/redisotel/v9"
	r "github.com/redis/go-redis/v9"
)

// Reexport go-redis's Nil constant for DX purposes.
const (
	Nil = r.Nil
)

type Client = r.Client

var (
	once                sync.Once
	client              *r.Client
	initializationError error
)

func New(ctx context.Context, c *config.Config) (*r.Client, error) {
	once.Do(func() {
		initializeClient(ctx, c)
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

func initializeClient(_ context.Context, c *config.Config) {
	client = r.NewClient(&r.Options{
		Addr:     fmt.Sprintf("%s:%d", c.RedisHost, c.RedisPort),
		Password: c.RedisPassword,
		DB:       c.RedisDatabase,
	})
}
