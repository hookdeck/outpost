package services_test

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/deliverymq"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/util/testinfra"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/hookdeck/outpost/internal/worker"
	natsgo "github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	amqp091 "github.com/rabbitmq/amqp091-go"
	"github.com/stretchr/testify/require"
)

// flakyBroker sits in the consumer's dial path: while down it refuses new
// connections, and going down drops the open ones.
type flakyBroker struct {
	mu    sync.Mutex
	down  bool
	conns []net.Conn
}

func (b *flakyBroker) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.down {
		return nil, &net.OpError{Op: "dial", Net: network, Err: errBrokerDown}
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	b.conns = append(b.conns, conn)
	return conn, nil
}

func (b *flakyBroker) setDown(down bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.down = down
	if down {
		for _, c := range b.conns {
			c.Close()
		}
		b.conns = nil
	}
}

var errBrokerDown = &net.AddrError{Err: "connection refused", Addr: "broker"}

type publishMQHarness struct {
	t          *testing.T
	supervisor *worker.WorkerSupervisor
	publisher  mqs.Queue
	received   chan string
	name       string
}

const publishMQWorkerName = "publishmq-consumer"

func startPublishMQWorker(t *testing.T, cfg *config.Config, consumerCfg, publisherCfg mqs.QueueConfig) *publishMQHarness {
	t.Helper()
	return startSupervisedConsumer(t, cfg, config.SupervisorWorkerPublishMQ, publishMQWorkerName,
		mqs.NewQueue(&consumerCfg).Subscribe, publisherCfg)
}

// startSupervisedConsumer runs one consumer worker, wired as the builder
// wires the worker under key, and publishes to publisherCfg.
func startSupervisedConsumer(
	t *testing.T,
	cfg *config.Config,
	key, name string,
	subscribe func(ctx context.Context, opts ...mqs.SubscribeOption) (mqs.Subscription, error),
	publisherCfg mqs.QueueConfig,
) *publishMQHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())

	publisher := mqs.NewQueue(&publisherCfg)
	cleanup, err := publisher.Init(ctx)
	require.NoError(t, err)

	h := &publishMQHarness{t: t, publisher: publisher, received: make(chan string, 100), name: name}
	handler := handlerFunc(func(ctx context.Context, msg *mqs.Message) error {
		msg.Ack()
		parsed := &testMsg{}
		if err := parsed.FromMessage(msg); err != nil {
			return err
		}
		h.received <- parsed.ID
		return nil
	})

	logger := testutil.CreateTestLogger(t)
	h.supervisor = worker.NewWorkerSupervisor(logger)
	w, registerOpts := services.NewSupervisedConsumerWorker(cfg, key, name, subscribe, handler, 1, logger)
	h.supervisor.Register(w, registerOpts...)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.supervisor.Run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
		cleanup()
	})
	return h
}

func (h *publishMQHarness) health() worker.WorkerHealth {
	return h.supervisor.GetHealthTracker().GetStatus()["workers"].(map[string]worker.WorkerHealth)[h.name]
}

func (h *publishMQHarness) awaitStatus(status string, timeout time.Duration) worker.WorkerHealth {
	h.t.Helper()
	var got worker.WorkerHealth
	require.Eventually(h.t, func() bool {
		got = h.health()
		return got.Status == status
	}, timeout, 100*time.Millisecond, "worker never became %s", status)
	return got
}

func (h *publishMQHarness) publish(id string) {
	h.t.Helper()
	require.NoError(h.t, h.publisher.Publish(context.Background(), &testMsg{ID: id}))
}

func (h *publishMQHarness) awaitMessage(id string, timeout time.Duration) {
	h.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case got := <-h.received:
			if got == id {
				return
			}
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s", id)
		}
	}
}

// The broker outage tests run on RabbitMQ only (TESTCOMPAT=1): its consumer
// fails when the connection drops, which is what the supervisor reacts to.
// The NATS client reconnects and re-pulls on its own, so an outage never
// reaches the supervisor and there is nothing to restart.
func TestIntegrationPublishMQRestart_BrokerOutage(t *testing.T) {
	testutil.SkipUnlessCompat(t)
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	cfg := testinfra.NewMQRabbitMQConfig(t)

	broker := &flakyBroker{}
	consumerCfg := withDialer(cfg, broker.dial)
	h := startPublishMQWorker(t, supervisorConfig(config.SupervisorWorkerPublishMQ), consumerCfg, cfg)
	runBrokerOutage(t, h, broker)
}

// The internal delivery queue gets the same restarts when enabled: RabbitMQ
// as the internal MQ, consumed through deliverymq as the delivery service does.
func TestIntegrationDeliveryMQRestart_BrokerOutage(t *testing.T) {
	testutil.SkipUnlessCompat(t)
	t.Parallel()
	t.Cleanup(testinfra.Start(t))
	cfg := testinfra.NewMQRabbitMQConfig(t)

	broker := &flakyBroker{}
	consumerCfg := withDialer(cfg, broker.dial)
	deliveryMQ := deliverymq.New(deliverymq.WithQueue(&consumerCfg))
	cleanup, err := deliveryMQ.Init(context.Background())
	require.NoError(t, err)
	t.Cleanup(cleanup)

	h := startSupervisedConsumer(t, supervisorConfig(config.SupervisorWorkerDeliveryMQ), config.SupervisorWorkerDeliveryMQ, "deliverymq-consumer",
		deliveryMQ.Subscribe, cfg)
	runBrokerOutage(t, h, broker)
}

func withDialer(cfg mqs.QueueConfig, dial func(ctx context.Context, network, addr string) (net.Conn, error)) mqs.QueueConfig {
	rabbit := *cfg.RabbitMQ
	rabbit.Dial = dial
	cfg.RabbitMQ = &rabbit
	return cfg
}

func runBrokerOutage(t *testing.T, h *publishMQHarness, broker *flakyBroker) {
	t.Helper()
	h.publish("before")
	h.awaitMessage("before", 10*time.Second)

	broker.setDown(true)
	got := h.awaitStatus(worker.WorkerStatusDegraded, 15*time.Second)
	require.NotNil(t, got.Since)

	h.publish("during")
	broker.setDown(false)

	h.awaitMessage("during", 30*time.Second)
	h.awaitStatus(worker.WorkerStatusHealthy, 45*time.Second)
}

// restartQueue is a queue the restart tests below run on: NATS by default,
// RabbitMQ with TESTCOMPAT=1. The /nats publish-MQ variants exercise the
// queue-agnostic supervisor; NATS isn't a publish queue provider.
type restartQueue struct {
	name   string
	compat bool
	// newConfig provisions a fresh queue.
	newConfig func(t *testing.T) mqs.QueueConfig
	// failingConfig returns a config for the same queue whose consumer
	// can't start.
	failingConfig func(t *testing.T, cfg mqs.QueueConfig) mqs.QueueConfig
	// deleteQueue removes the queue under a running consumer; redeclare
	// provisions it again.
	deleteQueue func(t *testing.T, cfg mqs.QueueConfig)
	redeclare   func(t *testing.T, cfg mqs.QueueConfig)
}

var restartQueues = []restartQueue{
	{
		name:      "nats",
		newConfig: testinfra.NewMQNATSConfig,
		failingConfig: func(t *testing.T, cfg mqs.QueueConfig) mqs.QueueConfig {
			unreachable := *cfg.NATS
			unreachable.ServerURL = "nats://127.0.0.1:1" // nothing listens on port 1
			cfg.NATS = &unreachable
			return cfg
		},
		deleteQueue: func(t *testing.T, cfg mqs.QueueConfig) {
			nc, err := natsgo.Connect(cfg.NATS.ServerURL)
			require.NoError(t, err)
			defer nc.Close()
			js, err := jetstream.New(nc)
			require.NoError(t, err)
			require.NoError(t, js.DeleteStream(context.Background(), cfg.NATS.Stream))
		},
		redeclare: func(t *testing.T, cfg mqs.QueueConfig) {
			infra := mqinfra.New(&mqinfra.MQInfraConfig{NATS: &mqinfra.NATSInfraConfig{
				ServerURL: cfg.NATS.ServerURL,
				Stream:    cfg.NATS.Stream,
				Subject:   cfg.NATS.Subject,
			}})
			require.NoError(t, infra.Declare(context.Background()))
		},
	},
	{
		name:          "rabbitmq",
		compat:        true,
		newConfig:     testinfra.NewMQRabbitMQConfig,
		failingConfig: badPasswordConfig,
		deleteQueue: func(t *testing.T, cfg mqs.QueueConfig) {
			conn, err := amqp091.Dial(cfg.RabbitMQ.ServerURL)
			require.NoError(t, err)
			defer conn.Close()
			ch, err := conn.Channel()
			require.NoError(t, err)
			_, err = ch.QueueDelete(cfg.RabbitMQ.Queue, false, false, false)
			require.NoError(t, err)
		},
		redeclare: func(t *testing.T, cfg mqs.QueueConfig) {
			require.NoError(t, testutil.DeclareTestRabbitMQInfrastructure(context.Background(), cfg.RabbitMQ))
		},
	},
}

// eachRestartQueue runs test once per queue, as parallel subtests.
func eachRestartQueue(t *testing.T, test func(t *testing.T, q restartQueue, cfg mqs.QueueConfig)) {
	t.Parallel()
	for _, q := range restartQueues {
		t.Run(q.name, func(t *testing.T) {
			if q.compat {
				testutil.SkipUnlessCompat(t)
			}
			t.Parallel()
			t.Cleanup(testinfra.Start(t))
			test(t, q, q.newConfig(t))
		})
	}
}

func TestIntegrationPublishMQRestart_QueueDeleted(t *testing.T) {
	eachRestartQueue(t, func(t *testing.T, q restartQueue, cfg mqs.QueueConfig) {
		h := startPublishMQWorker(t, supervisorConfig(config.SupervisorWorkerPublishMQ), cfg, cfg)
		h.publish("before")
		h.awaitMessage("before", 10*time.Second)

		q.deleteQueue(t, cfg)
		h.awaitStatus(worker.WorkerStatusDegraded, 15*time.Second)

		q.redeclare(t, cfg)
		h.publish("after")
		h.awaitMessage("after", 30*time.Second)
		h.awaitStatus(worker.WorkerStatusHealthy, 45*time.Second)
	})
}

func TestIntegrationPublishMQRestart_StartupFailure(t *testing.T) {
	eachRestartQueue(t, func(t *testing.T, q restartQueue, cfg mqs.QueueConfig) {
		start := time.Now()
		h := startPublishMQWorker(t, supervisorConfig(config.SupervisorWorkerPublishMQ), q.failingConfig(t, cfg), cfg)

		got := h.awaitStatus(worker.WorkerStatusDegraded, 5*time.Second)
		require.Equal(t, worker.ReasonStartupFailed, got.Reason)

		// 5 restarts at 1, 2, 4, 8, 16s (±20%): failed at ~31s plus the time
		// each failed start takes.
		got = h.awaitStatus(worker.WorkerStatusFailed, 60*time.Second)
		elapsed := time.Since(start)
		require.Greater(t, elapsed, 24*time.Second)
		require.Equal(t, worker.ReasonStartupFailed, got.Reason)
		require.False(t, h.supervisor.GetHealthTracker().IsHealthy())
	})
}

// supervisorConfig returns the default config with restarts on for the
// given workers only.
func supervisorConfig(restartWorkers ...string) *config.Config {
	cfg := &config.Config{}
	cfg.InitDefaults()
	cfg.PublishMaxConcurrency = 1
	cfg.Supervisor.RestartWorkers = restartWorkers
	return cfg
}

func badPasswordConfig(t *testing.T, cfg mqs.QueueConfig) mqs.QueueConfig {
	t.Helper()
	badRabbit := *cfg.RabbitMQ
	u, err := url.Parse(badRabbit.ServerURL)
	require.NoError(t, err)
	u.User = url.UserPassword(u.User.Username(), "wrong-password")
	badRabbit.ServerURL = u.String()
	cfg.RabbitMQ = &badRabbit
	return cfg
}

func TestIntegrationPublishMQRestart_DisabledByDefault(t *testing.T) {
	eachRestartQueue(t, func(t *testing.T, q restartQueue, cfg mqs.QueueConfig) {
		h := startPublishMQWorker(t, supervisorConfig(), q.failingConfig(t, cfg), cfg)

		// As without a restart policy: failed on the first error, no since/reason, no restart.
		got := h.awaitStatus(worker.WorkerStatusFailed, 15*time.Second)
		require.Equal(t, worker.WorkerHealth{Status: worker.WorkerStatusFailed}, got)
		require.False(t, h.supervisor.GetHealthTracker().IsHealthy())
	})
}

type handlerFunc func(ctx context.Context, msg *mqs.Message) error

func (f handlerFunc) Handle(ctx context.Context, msg *mqs.Message) error { return f(ctx, msg) }

type testMsg struct {
	ID string
}

func (m *testMsg) FromMessage(msg *mqs.Message) error {
	return json.Unmarshal(msg.Body, m)
}

func (m *testMsg) ToMessage() (*mqs.Message, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return &mqs.Message{Body: data}, nil
}
