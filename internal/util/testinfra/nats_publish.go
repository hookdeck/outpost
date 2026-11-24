package testinfra

import (
	"context"
	"log"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
)

// NewMQNATSConfig spins up (if necessary) a NATS JetStream test container,
// provisions one stream + consumer with random names, and returns a
// QueueConfig with one account pointing at them. The stream/consumer are
// torn down after the test.
// NewMQNATSPublishConfig returns a publishmq NATS config with one account on
// a fresh stream and consumer, on the shared JetStream test server.
func NewMQNATSPublishConfig(t *testing.T) mqs.QueueConfig {
	stream := "test-" + uuid.New().String()
	consumer := "test-" + uuid.New().String()

	cfg := mqs.QueueConfig{
		NATSPublish: &mqs.NATSPublishConfig{
			Servers: []string{EnsureNATS(t)},
			Accounts: []mqs.NATSPublishAccountConfig{{
				Name:     "test",
				Stream:   stream,
				Consumer: consumer,
			}},
		},
	}

	ctx := context.Background()
	if err := testutil.DeclareTestNATSInfrastructure(ctx, cfg.NATSPublish); err != nil {
		panic(err)
	}
	t.Cleanup(func() {
		if err := testutil.TeardownTestNATSInfrastructure(ctx, cfg.NATSPublish); err != nil {
			log.Println("Failed to teardown NATS infrastructure", err)
		}
	})
	return cfg
}
