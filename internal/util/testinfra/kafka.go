package testinfra

import (
	"context"
	"log"
	"net"
	"net/netip"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/segmentio/kafka-go"
	"github.com/segmentio/kafka-go/sasl/plain"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// kafkaJaasPath is where the broker's JAAS config lands inside the container,
// matching the mount path in build/test/compose.yml.
const kafkaJaasPath = "/etc/kafka/jaas.conf"

var (
	kafkaOnce      sync.Once
	kafkaReadyOnce sync.Once
)

func EnsureKafka() string {
	cfg := ReadConfig()
	if cfg.KafkaURL == "" {
		kafkaOnce.Do(func() {
			startKafkaTestContainer(cfg)
		})
	}
	// A broker opens 9092 while it is still starting up and drops the SASL
	// handshake, so authenticate rather than dial.
	kafkaReadyOnce.Do(func() {
		waitReadyLogged("kafka", cfg.KafkaURL, func() error {
			dialer := &kafka.Dialer{
				SASLMechanism: plain.Mechanism{Username: "admin", Password: "admin-secret"},
				Timeout:       5 * time.Second,
			}
			conn, err := dialer.DialContext(context.Background(), "tcp", cfg.KafkaURL)
			if err != nil {
				return err
			}
			defer conn.Close()
			_, err = conn.Brokers()
			return err
		})
	})
	return cfg.KafkaURL
}

func startKafkaTestContainer(cfg *Config) {
	ctx := context.Background()

	// A Kafka broker hands clients the address in its advertised listener, so the
	// host port has to be known before the container starts — testcontainers'
	// usual "map to whatever is free and ask afterwards" does not work here.
	// Claim a free port from the OS and bind the container to it explicitly.
	// Reserving one per process, rather than hardcoding a port, keeps two test
	// binaries that both want Kafka from fighting over the same number.
	hostPort := reserveHostPort()

	jaasConfig := `org.apache.kafka.common.security.plain.PlainLoginModule required username="admin" password="admin-secret" user_admin="admin-secret";`

	// The broker refuses to start without a KafkaServer entry in a JAAS file,
	// whatever the per-listener SASL config says, so mount the same file the
	// compose stack mounts.
	projectRoot, err := findProjectRoot()
	if err != nil {
		panic(err)
	}
	jaasFile := filepath.Join(projectRoot, "build", "test", "kafka_jaas.conf")

	req := testcontainers.ContainerRequest{
		Image:        cfg.Images.Kafka,
		ExposedPorts: []string{"9092/tcp"},
		Files: []testcontainers.ContainerFile{{
			HostFilePath:      jaasFile,
			ContainerFilePath: kafkaJaasPath,
			FileMode:          0o644,
		}},
		HostConfigModifier: func(hostConfig *container.HostConfig) {
			hostConfig.PortBindings = network.PortMap{
				network.MustParsePort("9092/tcp"): []network.PortBinding{
					{HostIP: netip.IPv4Unspecified(), HostPort: hostPort},
				},
			}
		},
		Env: map[string]string{
			// Advertise 127.0.0.1, not localhost: the published port is bound on
			// IPv4 only, and a client that resolves localhost to ::1 first — the
			// default on macOS — gets connection refused.
			"KAFKA_ADVERTISED_LISTENERS":           "SASL_PLAINTEXT://localhost:29092,SASL_PLAINTEXT_HOST://127.0.0.1:" + hostPort,
			"KAFKA_LISTENER_SECURITY_PROTOCOL_MAP": "SASL_PLAINTEXT:SASL_PLAINTEXT,SASL_PLAINTEXT_HOST:SASL_PLAINTEXT,CONTROLLER:PLAINTEXT",
			// The controller listener has to stay on the address the image's
			// default quorum voters name, or the broker starts, fails to register
			// with the quorum, and shuts down again with the port already open.
			"KAFKA_LISTENERS":                                                  "SASL_PLAINTEXT://0.0.0.0:29092,CONTROLLER://localhost:29093,SASL_PLAINTEXT_HOST://0.0.0.0:9092",
			"KAFKA_INTER_BROKER_LISTENER_NAME":                                 "SASL_PLAINTEXT",
			"KAFKA_SASL_MECHANISM_INTER_BROKER_PROTOCOL":                       "PLAIN",
			"KAFKA_SASL_ENABLED_MECHANISMS":                                    "PLAIN",
			"KAFKA_LISTENER_NAME_SASL__PLAINTEXT_PLAIN_SASL_JAAS_CONFIG":       jaasConfig,
			"KAFKA_LISTENER_NAME_SASL__PLAINTEXT__HOST_PLAIN_SASL_JAAS_CONFIG": jaasConfig,
			"KAFKA_OPTS":                                     "-Djava.security.auth.login.config=" + kafkaJaasPath,
			"KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR":         "1",
			"KAFKA_TRANSACTION_STATE_LOG_MIN_ISR":            "1",
			"KAFKA_TRANSACTION_STATE_LOG_REPLICATION_FACTOR": "1",
		},
		WaitingFor: wait.ForListeningPort("9092/tcp"),
	}

	_, err = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(err)
	}

	endpoint := "127.0.0.1:" + hostPort
	log.Printf("Kafka running at %s", endpoint)
	cfg.KafkaURL = endpoint
}

// reserveHostPort returns a port the OS reports as free. The listener is closed
// before the port is used, so this is a hint rather than a reservation — good
// enough for picking a broker port, not for anything that must not be raced.
func reserveHostPort() string {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer listener.Close()
	return strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
}
