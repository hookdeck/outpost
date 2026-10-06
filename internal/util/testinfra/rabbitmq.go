package testinfra

import (
	"context"
	"log"
	"testing"

	amqp091 "github.com/rabbitmq/amqp091-go"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/testcontainers/testcontainers-go/modules/rabbitmq"
)

func NewMQRabbitMQConfig(t *testing.T) mqs.QueueConfig {
	queueConfig := mqs.QueueConfig{
		RabbitMQ: &mqs.RabbitMQConfig{
			ServerURL: EnsureRabbitMQ(t),
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

var rabbitmqService = &service{name: "rabbitmq", startHint: hintDest}

// EnsureRabbitMQ returns the AMQP URL of the test RabbitMQ, failing t if it
// isn't available.
func EnsureRabbitMQ(t testing.TB) string {
	t.Helper()
	cfg := ReadConfig()
	// Dial rather than probe the port: RabbitMQ accepts TCP before it will
	// complete an AMQP handshake, and it is the handshake that tests need.
	return rabbitmqService.ensure(t, cfg.RabbitMQURL,
		func() (string, error) { return startRabbitMQTestContainer(cfg) },
		func(endpoint string) error {
			conn, err := amqp091.Dial(endpoint)
			if err != nil {
				return err
			}
			return conn.Close()
		})
}

func startRabbitMQTestContainer(cfg *Config) (string, error) {
	ctx := context.Background()

	rabbitmqContainer, err := rabbitmq.Run(ctx, cfg.Images.RabbitMQ)
	if err != nil {
		return "", err
	}

	endpoint, err := rabbitmqContainer.PortEndpoint(ctx, "5672/tcp", "")
	if err != nil {
		return "", err
	}
	log.Printf("RabbitMQ running at %s", endpoint)
	return "amqp://guest:guest@" + endpoint, nil
}
