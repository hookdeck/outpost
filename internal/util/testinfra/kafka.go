package testinfra

import (
	"context"
	"log"
	"sync"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var kafkaOnce sync.Once

func EnsureKafka() string {
	cfg := ReadConfig()
	if cfg.KafkaURL == "" {
		kafkaOnce.Do(func() {
			startKafkaTestContainer(cfg)
		})
	}
	return cfg.KafkaURL
}

func startKafkaTestContainer(cfg *Config) {
	ctx := context.Background()

	// Use a fixed host port so advertised listeners match what clients connect to.
	// Kafka brokers redirect clients to the advertised address, so if testcontainers
	// maps to a random port but the broker advertises 9092, clients would fail.
	const hostPort = "19092"

	req := testcontainers.ContainerRequest{
		Image:        "confluentinc/confluent-local:7.4.0",
		ExposedPorts: []string{hostPort + ":9092/tcp"},
		Env: map[string]string{
			"KAFKA_ADVERTISED_LISTENERS":                     "PLAINTEXT://localhost:29092,PLAINTEXT_HOST://localhost:" + hostPort,
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP":           "PLAINTEXT:PLAINTEXT,PLAINTEXT_HOST:PLAINTEXT,CONTROLLER:PLAINTEXT",
			"KAFKA_LISTENERS":                                "PLAINTEXT://0.0.0.0:29092,PLAINTEXT_HOST://0.0.0.0:9092,CONTROLLER://0.0.0.0:9093",
			"KAFKA_INTER_BROKER_LISTENER_NAME":               "PLAINTEXT",
			"KAFKA_CONTROLLER_LISTENER_NAMES":                "CONTROLLER",
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
		},
		WaitingFor: wait.ForListeningPort("9092/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(err)
	}

	endpoint := "localhost:" + hostPort
	log.Printf("Kafka running at %s", endpoint)
	cfg.KafkaURL = endpoint
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate kafka container: %s", err)
		}
	})
}
