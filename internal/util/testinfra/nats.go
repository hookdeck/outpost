package testinfra

import (
	"context"
	"log"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// NewMQNATSConfig spins up (if necessary) a NATS JetStream test container,
// provisions one stream + consumer with random names, and returns a
// QueueConfig with one account pointing at them. The stream/consumer are
// torn down after the test.
func NewMQNATSConfig(t *testing.T) mqs.QueueConfig {
	stream := "test-" + uuid.New().String()
	consumer := "test-" + uuid.New().String()

	cfg := mqs.QueueConfig{
		NATS: &mqs.NATSConfig{
			Servers: []string{EnsureNATS()},
			Accounts: []mqs.NATSAccountConfig{{
				Name:     "test",
				Stream:   stream,
				Consumer: consumer,
			}},
		},
	}

	ctx := context.Background()
	if err := testutil.DeclareTestNATSInfrastructure(ctx, cfg.NATS); err != nil {
		panic(err)
	}
	t.Cleanup(func() {
		if err := testutil.TeardownTestNATSInfrastructure(ctx, cfg.NATS); err != nil {
			log.Println("Failed to teardown NATS infrastructure", err)
		}
	})
	return cfg
}

var natsOnce sync.Once

// EnsureNATS returns a NATS URL, starting a test container on first call
// unless TEST_NATS_URL is set in the test environment.
//
// The check for an existing NATSURL is performed *inside* sync.Once.Do so
// concurrent callers from t.Parallel() tests don't race against the
// container-start path (which writes cfg.NATSURL).
func EnsureNATS() string {
	cfg := ReadConfig()
	natsOnce.Do(func() {
		if cfg.NATSURL == "" {
			startNATSTestContainer(cfg)
		}
	})
	return cfg.NATSURL
}

func startNATSTestContainer(cfg *Config) {
	ctx := context.Background()

	req := testcontainers.ContainerRequest{
		Image:        "nats:2.10-alpine",
		ExposedPorts: []string{"4222/tcp"},
		Cmd:          []string{"-js"},
		WaitingFor:   wait.ForListeningPort("4222/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		panic(err)
	}

	endpoint, err := container.PortEndpoint(ctx, "4222/tcp", "nats")
	if err != nil {
		panic(err)
	}
	log.Printf("NATS running at %s", endpoint)
	cfg.NATSURL = endpoint
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := container.Terminate(ctx); err != nil {
			log.Printf("failed to terminate nats container: %s", err)
		}
	})
}
