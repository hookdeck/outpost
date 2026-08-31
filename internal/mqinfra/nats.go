package mqinfra

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

type infraNATS struct {
	cfg *MQInfraConfig
}

// DefaultNATSDLQName must not nest under the main stream's subject
// namespace (e.g. "outpost.>") — a DLQ subject like "outpost.delivery.dlq"
// would overlap with that wildcard, and JetStream forbids two streams from
// claiming overlapping subjects. Using an unrelated "dlq." prefix keeps the
// DLQ stream's subject space entirely separate.
func DefaultNATSDLQName(subject string) string {
	return "dlq." + sanitizeName(subject)
}

func (infra *infraNATS) dlqSubject() string {
	if infra.cfg.NATS.DLQ != "" {
		return infra.cfg.NATS.DLQ
	}
	return DefaultNATSDLQName(infra.cfg.NATS.Subject)
}

// sanitizeName makes a subject safe to use as a JetStream stream/consumer
// name — unlike subjects, names can't contain '.', '*', '>' or whitespace.
func sanitizeName(subject string) string {
	return strings.NewReplacer(".", "-", "*", "-", ">", "-", " ", "-").Replace(subject)
}

func (infra *infraNATS) durableName() string {
	return sanitizeName(infra.cfg.NATS.Subject) + "-consumer"
}

func (infra *infraNATS) connect() (*nats.Conn, jetstream.JetStream, error) {
	nc, err := nats.Connect(infra.cfg.NATS.ServerURL)
	if err != nil {
		return nil, nil, err
	}
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		return nil, nil, err
	}
	return nc, js, nil
}

func (infra *infraNATS) Exist(ctx context.Context) (bool, error) {
	if infra.cfg == nil || infra.cfg.NATS == nil {
		return false, errors.New("failed assertion: cfg.NATS != nil") // IMPOSSIBLE
	}

	nc, js, err := infra.connect()
	if err != nil {
		return false, err
	}
	defer nc.Close()

	if _, err := js.Stream(ctx, infra.cfg.NATS.Stream); err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			return false, nil
		}
		return false, err
	}

	dlqStream := infra.dlqStreamName()
	if _, err := js.Stream(ctx, dlqStream); err != nil {
		if errors.Is(err, jetstream.ErrStreamNotFound) {
			return false, nil
		}
		return false, err
	}

	if _, err := js.Consumer(ctx, infra.cfg.NATS.Stream, infra.durableName()); err != nil {
		if errors.Is(err, jetstream.ErrConsumerNotFound) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (infra *infraNATS) dlqStreamName() string {
	return sanitizeName(infra.dlqSubject())
}

func (infra *infraNATS) Declare(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.NATS == nil {
		return errors.New("failed assertion: cfg.NATS != nil") // IMPOSSIBLE
	}

	nc, js, err := infra.connect()
	if err != nil {
		return err
	}
	defer nc.Close()

	// deliverymq and logmq share one stream but declare it separately, each
	// with only its own subject — a plain single-subject list here would let
	// whichever one declares second silently wipe out the other's subject
	// (UpdateStream replaces the list, it doesn't merge). A wildcard scoped
	// to the stream name keeps both declarations identical and idempotent.
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      infra.cfg.NATS.Stream,
		Subjects:  []string{infra.cfg.NATS.Stream + ".>"},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		return err
	}

	dlq := infra.dlqSubject()
	if _, err := js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      infra.dlqStreamName(),
		Subjects:  []string{dlq},
		Retention: jetstream.WorkQueuePolicy,
		Storage:   jetstream.FileStorage,
	}); err != nil {
		return err
	}

	maxDeliver := infra.cfg.Policy.RetryLimit
	if maxDeliver <= 0 {
		maxDeliver = 5
	}

	if _, err := js.CreateOrUpdateConsumer(ctx, infra.cfg.NATS.Stream, jetstream.ConsumerConfig{
		Durable:       infra.durableName(),
		FilterSubject: infra.cfg.NATS.Subject,
		AckPolicy:     jetstream.AckExplicitPolicy,
		AckWait:       60 * time.Second,
		MaxDeliver:    maxDeliver,
	}); err != nil {
		return err
	}

	return nil
}

func (infra *infraNATS) TearDown(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.NATS == nil {
		return errors.New("failed assertion: cfg.NATS != nil") // IMPOSSIBLE
	}

	nc, js, err := infra.connect()
	if err != nil {
		return err
	}
	defer nc.Close()

	if err := js.DeleteStream(ctx, infra.cfg.NATS.Stream); err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}
	if err := js.DeleteStream(ctx, infra.dlqStreamName()); err != nil && !errors.Is(err, jetstream.ErrStreamNotFound) {
		return err
	}

	return nil
}
