package testinfra

import (
	"context"
	"log"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

func NewMQRabbitMQConfig(t *testing.T) mqs.QueueConfig {
	queueConfig := mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: EnsureRabbitMQ(),
			Exchange:  uuid.New().String(),
			Queue:     uuid.New().String(),
		},
	}
	ctx := context.Background()
	if err := testutil.DeclareTestRabbitMQInfrastructure(ctx, queueConfig.RabbitMQ); err != nil {
		panic(err)
	}
	t.Cleanup(func() {
		if err := testutil.TeardownTestRabbitMQInfrastructure(ctx, queueConfig.RabbitMQ); err != nil {
			log.Println("Failed to teardown RabbitMQ infrastructure", err, *queueConfig.RabbitMQ)
		}
	})
	return queueConfig
}

var rabbitmqOnce sync.Once

func EnsureRabbitMQ() string {
	cfg := ReadConfig()
	if cfg.RabbitMQURL == "" {
		rabbitmqOnce.Do(func() {
			startRabbitMQTestContainer(cfg)
		})
	}
	return cfg.RabbitMQURL
}

func startRabbitMQTestContainer(cfg *Config) {
	ctx := context.Background()

	rabbitmqContainer, err := rabbitmq.Run(ctx,
		"rabbitmq:3-management-alpine",
	)
	if err != nil {
		panic(err)
	}

	endpoint, err := rabbitmqContainer.PortEndpoint(ctx, "5672/tcp", "")
	if err != nil {
		panic(err)
	}
	log.Printf("RabbitMQ running at %s", endpoint)
	cfg.RabbitMQURL = "amqp://guest:guest@" + endpoint
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := rabbitmqContainer.Terminate(ctx); err != nil {
			log.Printf("failed to terminate container: %s", err)
		}
	})
}
