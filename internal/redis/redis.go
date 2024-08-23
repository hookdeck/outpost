package redis

import (
	"errors"
	"fmt"
	"sync"

	"github.com/hookdeck/EventKit/internal/config"
	"github.com/redis/go-redis/extra/redisotel/v9"
	r "github.com/redis/go-redis/v9"
)

var (
	client *r.Client
	once   sync.Once
)

const (
	Nil = r.Nil
)

func InstrumentOpenTelemetry() error {
	once.Do(initializeClient)

	if config.OpenTelemetry == nil {
		return errors.New("OpenTelemetry config is nil")
	}
	if config.OpenTelemetry.Traces != nil {
		if err := redisotel.InstrumentTracing(client); err != nil {
			return err
		}
	}
	if config.OpenTelemetry.Metrics != nil {
		if err := redisotel.InstrumentMetrics(client); err != nil {
			return err
		}
	}

	return nil
}

func Client() *r.Client {
	once.Do(initializeClient)
	return client
}

func initializeClient() {
	client = r.NewClient(&r.Options{
		Addr:     fmt.Sprintf("%s:%d", config.RedisHost, config.RedisPort),
		Password: config.RedisPassword,
		DB:       config.RedisDatabase,
	})
}
