package main

import (
	"context"
	"errors"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"

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
	dialer := &kafka.Dialer{Timeout: 5 * time.Second}
	if username != "" {
		dialer.SASLMechanism = plain.Mechanism{Username: username, Password: password}
	}

	if err := createTopic(dialer); err != nil {
		return err
	}

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     []string{brokerAddr},
		Topic:       topic,
		Dialer:      dialer,
		StartOffset: kafka.LastOffset,
	})
	defer reader.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("[*] Ready to receive messages.\n\tBroker: %s\n\tTopic: %s", brokerAddr, topic)
	log.Printf("[*] In a destination of the make up stack use brokers dest-kafka:29094 (SASL PLAIN %s/%s) or dest-kafka:29092 (plaintext)", username, password)
	log.Printf("[*] Waiting for messages. To exit press CTRL+C")
	for {
		m, err := reader.ReadMessage(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		log.Printf("[x] partition %d offset %d key %q: %s", m.Partition, m.Offset, m.Key, m.Value)
	}
}

// createTopic creates the topic through the controller, waiting for the
// broker while it starts. An existing topic is fine.
func createTopic(dialer *kafka.Dialer) error {
	deadline := time.Now().Add(60 * time.Second)
	for {
		err := tryCreateTopic(dialer)
		if err == nil || errors.Is(err, kafka.TopicAlreadyExists) {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		log.Printf("[*] waiting for Kafka at %s: %v", brokerAddr, err)
		time.Sleep(2 * time.Second)
	}
}

func tryCreateTopic(dialer *kafka.Dialer) error {
	ctx := context.Background()
	conn, err := dialer.DialContext(ctx, "tcp", brokerAddr)
	if err != nil {
		return err
	}
	defer conn.Close()
	controller, err := conn.Controller()
	if err != nil {
		return err
	}
	cconn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(brokerHost(), strconv.Itoa(controller.Port)))
	if err != nil {
		return err
	}
	defer cconn.Close()
	return cconn.CreateTopics(kafka.TopicConfig{Topic: topic, NumPartitions: 1, ReplicationFactor: 1})
}

// brokerHost is the host part of brokerAddr. The controller reports its
// advertised host, which for the stack's host listener is localhost anyway;
// using ours keeps a remote DEST_KAFKA_BROKER working.
func brokerHost() string {
	host, _, err := net.SplitHostPort(brokerAddr)
	if err != nil {
		return brokerAddr
	}
	return host
}
