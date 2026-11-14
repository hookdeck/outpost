package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/sasl/plain"

	"github.com/hookdeck/outpost/cmd/destinations/internal/destenv"
)

// Creates a topic on the Kafka of the local destination stack (`make up/dest`,
// which doesn't auto-create topics) and prints every message on it. Defaults
// use the stack's SASL listener; set DEST_KAFKA_USERNAME="" for a broker
// without SASL.
var (
	brokerAddr = destenv.Get("DEST_KAFKA_BROKER", "localhost:19093")
	topic      = destenv.Get("DEST_KAFKA_TOPIC", "destination-topic")
	username   = destenv.Get("DEST_KAFKA_USERNAME", "admin")
	password   = destenv.Get("DEST_KAFKA_PASSWORD", "admin-secret")
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	opts := []kgo.Opt{
		kgo.SeedBrokers(brokerAddr),
		kgo.DialTimeout(5 * time.Second),
	}
	if username != "" {
		opts = append(opts, kgo.SASL(plain.Auth{User: username, Pass: password}.AsMechanism()))
	}

	admin, err := kgo.NewClient(opts...)
	if err != nil {
		return err
	}
	defer admin.Close()
	if err := createTopic(kadm.NewClient(admin)); err != nil {
		return err
	}

	consumer, err := kgo.NewClient(append(opts,
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
	)...)
	if err != nil {
		return err
	}
	defer consumer.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("[*] Ready to receive messages.\n\tBroker: %s\n\tTopic: %s", brokerAddr, topic)
	log.Printf("[*] In a destination of the make up stack use brokers dest-kafka:29094 (SASL PLAIN %s/%s) or dest-kafka:29092 (plaintext)", username, password)
	log.Printf("[*] Waiting for messages. To exit press CTRL+C")
	for {
		fetches := consumer.PollFetches(ctx)
		if fetches.IsClientClosed() || ctx.Err() != nil {
			return nil
		}
		fetches.EachError(func(t string, p int32, err error) {
			log.Printf("[!] fetch %s/%d: %v", t, p, err)
		})
		fetches.EachRecord(func(m *kgo.Record) {
			log.Printf("[x] partition %d offset %d key %q: %s", m.Partition, m.Offset, m.Key, m.Value)
		})
	}
}

// createTopic creates the topic through the controller, waiting for the
// broker while it starts. An existing topic is fine.
func createTopic(admin *kadm.Client) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		_, err := admin.CreateTopic(ctx, 1, 1, nil, topic)
		cancel()
		if err == nil || errors.Is(err, kerr.TopicAlreadyExists) {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		log.Printf("[*] waiting for Kafka at %s: %v", brokerAddr, err)
		time.Sleep(2 * time.Second)
	}
}
