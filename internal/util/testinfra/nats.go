package testinfra

import (
	"context"
	"log"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewMQNATSConfig provisions a fresh, uniquely-named stream/subject/consumer
// via mqinfra (not a hand-rolled declare, unlike the RabbitMQ helper) —
// NATSQueue.Subscribe now looks up a consumer mqinfra already provisioned
// rather than creating one itself, so tests need that same real
// provisioning path to get a working subscription at all.
func NewMQNATSConfig(t *testing.T) mqs.QueueConfig {
	serverURL := EnsureNATS(t)
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

// SetNATSAckWait sets how long the consumers of cfg's stream wait for an ack
// before redelivering: NATS's counterpart of a queue's visibility timeout.
// Outpost provisions 60 s, too long for a test that waits for a redelivery.
func SetNATSAckWait(t testing.TB, cfg mqs.QueueConfig, ackWait time.Duration) {
	t.Helper()
	ctx := context.Background()
	nc, err := natsgo.Connect(cfg.NATS.ServerURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	stream, err := js.Stream(ctx, cfg.NATS.Stream)
	if err != nil {
		t.Fatal(err)
	}
	names := stream.ConsumerNames(ctx)
	for name := range names.Name() {
		consumer, err := stream.Consumer(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		consumerCfg := consumer.CachedInfo().Config
		consumerCfg.AckWait = ackWait
		if _, err := js.UpdateConsumer(ctx, cfg.NATS.Stream, consumerCfg); err != nil {
			t.Fatal(err)
		}
	}
	if err := names.Err(); err != nil {
		t.Fatal(err)
	}
}

var natsService = &service{name: "nats", startHint: hintTest}

// EnsureNATS returns the URL of the test NATS server (JetStream enabled),
// failing t if it isn't available.
func EnsureNATS(t testing.TB) string {
	t.Helper()
	cfg := ReadConfig()
	return natsService.ensure(t, cfg.NATSURL,
		func() (string, error) { return startNATSTestContainer(cfg) },
		func(endpoint string) error {
			nc, err := natsgo.Connect(endpoint)
			if err != nil {
				return err
			}
			nc.Close()
			return nil
		})
}

func startNATSTestContainer(cfg *Config) (string, error) {
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
		return "", err
	}

	endpoint, err := natsContainer.PortEndpoint(ctx, "4222/tcp", "")
	if err != nil {
		return "", err
	}
	log.Printf("NATS running at %s", endpoint)
	return endpoint, nil
}
