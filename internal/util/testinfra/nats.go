package testinfra

import (
	"context"
	"log"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
	natsgo "github.com/nats-io/nats.go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewMQNATSConfig provisions a fresh, uniquely-named stream/subject/consumer
// via mqinfra (not a hand-rolled declare, unlike the RabbitMQ helper) —
// NATSQueue.Subscribe now looks up a consumer mqinfra already provisioned
// rather than creating one itself, so tests need that same real
// provisioning path to get a working subscription at all.
func NewMQNATSConfig(t *testing.T) mqs.QueueConfig {
	serverURL := EnsureNATS()
	stream := "test" + uuid.New().String()[:8]
	subject := stream + ".events"

	infra := mqinfra.New(&mqinfra.MQInfraConfig{
		NATS: &mqinfra.NATSInfraConfig{
			ServerURL: serverURL,
			Stream:    stream,
			Subject:   subject,
		},
	})
	ctx := context.Background()
	if err := infra.Declare(ctx); err != nil {
		panic(err)
	}
	t.Cleanup(func() {
		if err := infra.TearDown(ctx); err != nil {
			log.Println("Failed to teardown NATS infrastructure", err, stream)
		}
	})

	return mqs.QueueConfig{
		NATS: &mqs.NATSConfig{
			ServerURL: serverURL,
			Stream:    stream,
			Subject:   subject,
		},
	}
}

var (
	natsOnce      sync.Once
	natsReadyOnce sync.Once
)

func EnsureNATS() string {
	cfg := ReadConfig()
	if cfg.NATSURL == "" {
		natsOnce.Do(func() {
			startNATSTestContainer(cfg)
		})
	}
	natsReadyOnce.Do(func() {
		waitReadyLogged("nats", cfg.NATSURL, func() error {
			nc, err := natsgo.Connect(cfg.NATSURL)
			if err != nil {
				return err
			}
			nc.Close()
			return nil
		})
	})
	return cfg.NATSURL
}

func startNATSTestContainer(cfg *Config) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        cfg.Images.NATS,
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-js"}, // enable JetStream; plain NATS has no persistence
		WaitingFor:   wait.ForListeningPort("4222/tcp"),
	}

	natsContainer, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(err)
	}

	endpoint, err := natsContainer.PortEndpoint(ctx, "4222/tcp", "")
	if err != nil {
		panic(err)
	}
	log.Printf("NATS running at %s", endpoint)
	cfg.NATSURL = endpoint
}
