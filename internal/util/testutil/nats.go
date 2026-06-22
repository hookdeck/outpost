package testutil

import (
	"context"
	"errors"
	"strings"

	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// DeclareTestNATSInfrastructure creates the JetStream stream + durable
// consumer named in cfg.Accounts[*] so the NATS queue driver can connect.
// Each account is set up with a single subject "<stream>.events".
func DeclareTestNATSInfrastructure(ctx context.Context, cfg *mqs.NATSConfig) error {
	servers := strings.Join(cfg.Servers, ",")
	nc, err := nats.Connect(servers)
	if err != nil {
		return err
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	for _, acc := range cfg.Accounts {
		subject := acc.Stream + ".events"
		stream, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
			Name:      acc.Stream,
			Subjects:  []string{subject},
			Retention: jetstream.WorkQueuePolicy,
			Storage:   jetstream.MemoryStorage,
		})
		if err != nil {
			return err
		}
		if _, err := stream.CreateOrUpdateConsumer(ctx, jetstream.ConsumerConfig{
			Durable:       acc.Consumer,
			AckPolicy:     jetstream.AckExplicitPolicy,
			FilterSubject: subject,
		}); err != nil {
			return err
		}
	}
	return nil
}

// TeardownTestNATSInfrastructure removes the streams created by Declare.
func TeardownTestNATSInfrastructure(ctx context.Context, cfg *mqs.NATSConfig) error {
	servers := strings.Join(cfg.Servers, ",")
	nc, err := nats.Connect(servers)
	if err != nil {
		return err
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}

	for _, acc := range cfg.Accounts {
		if err := js.DeleteStream(ctx, acc.Stream); err != nil {
			// Best-effort teardown; ignore "not found" so re-runs don't fail.
			if !errors.Is(err, jetstream.ErrStreamNotFound) {
				return err
			}
		}
	}
	return nil
}

// PublishToNATSStream publishes a JSON payload to a stream's events subject.
// Used by tests to inject events for the queue driver to consume.
func PublishToNATSStream(ctx context.Context, servers []string, stream string, payload []byte) error {
	nc, err := nats.Connect(strings.Join(servers, ","))
	if err != nil {
		return err
	}
	defer nc.Close()

	js, err := jetstream.New(nc)
	if err != nil {
		return err
	}
	_, err = js.Publish(ctx, stream+".events", payload)
	return err
}
