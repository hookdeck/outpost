package providers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
	"github.com/hookdeck/outpost/internal/mqinfra"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"
)

// GCP Pub/Sub, on the emulator (default, `make up`) or a real project.
//
// Broker semantics used here (https://cloud.google.com/pubsub/docs):
//   - per-message ack deadline; an unacked message is redelivered after it;
//   - flow control (max outstanding messages / bytes) is enforced by the
//     server on the streaming pull; the emulator enforces none of it and its
//     lease timing is not the service's, so flow-control, lease-timing and
//     capacity checks are not observable on the emulator;
//   - dead-letter policy forwards after max delivery attempts (minimum 5); on
//     a real project the Pub/Sub service agent needs publish rights on the
//     dead-letter topic and subscribe rights on the subscription;
//   - no live in-flight count (Cloud Monitoring lags minutes);
//   - at-least-once delivery: occasional broker duplicates.

const (
	gcpEnvProject     = "MQCHECK_GCP_PROJECT"
	gcpEnvEmulator    = "MQCHECK_GCP_EMULATOR_HOST"
	gcpEnvCredentials = "MQCHECK_GCP_CREDENTIALS_FILE"
)

func init() {
	provider.Register(provider.Registration{
		Name:    "gcppubsub",
		Summary: "GCP Pub/Sub: the emulator from `make up` by default, or a real project by config",
		Settings: []provider.Setting{
			{Env: gcpEnvProject, Default: "mqcheck", Description: "Project id. Any id works on the emulator."},
			{Env: gcpEnvEmulator, Default: "localhost:48085", Description: `Emulator host:port. "off" targets the real Pub/Sub service in the project.`},
			{Env: gcpEnvCredentials, Description: "Service account key file (JSON) for a real project. Unset uses Application Default Credentials."},
		},
		PassEnv: []string{"GOOGLE_APPLICATION_CREDENTIALS", "CLOUDSDK_CONFIG"},
		Open:    openGCP,
	})
}

type gcpProvider struct {
	provider.OutpostPublisher
	project  string
	emulator string // host:port, "" = real service
	credJSON string
	client   *pubsub.Client

	mu   sync.Mutex
	dlqs map[string]*pubsub.Subscription
}

func openGCP(ctx context.Context, cfg provider.Config) (provider.Provider, error) {
	p := &gcpProvider{project: cfg.Get(gcpEnvProject), emulator: cfg.Get(gcpEnvEmulator), dlqs: map[string]*pubsub.Subscription{}}
	if p.emulator == "off" {
		p.emulator = ""
	}
	if p.emulator != "" {
		// Read by the Pub/Sub clients in this process (admin, publisher).
		os.Setenv("PUBSUB_EMULATOR_HOST", p.emulator)
	} else {
		os.Unsetenv("PUBSUB_EMULATOR_HOST")
	}
	var opts []option.ClientOption
	if f := cfg.Get(gcpEnvCredentials); f != "" && p.emulator == "" {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", gcpEnvCredentials, err)
		}
		p.credJSON = string(b)
		opts = append(opts, option.WithCredentialsJSON(b))
	}
	client, err := pubsub.NewClient(ctx, p.project, opts...)
	if err != nil {
		return nil, err
	}
	p.client = client
	it := client.Topics(ctx)
	if _, err := it.Next(); err != nil && !errors.Is(err, iterator.Done) {
		client.Close()
		return nil, fmt.Errorf("reach Pub/Sub (%s): %w", p.where(), err)
	}
	p.OutpostPublisher.Env = p.WorkerEnv
	return p, nil
}

func (p *gcpProvider) where() string {
	if p.emulator != "" {
		return "emulator at " + p.emulator
	}
	return "project " + p.project
}

func (p *gcpProvider) Caps() provider.Caps {
	c := provider.Caps{
		InFlight:        provider.AccuracyNone,
		MaxMessageBytes: 10 << 20,
		// Outpost's Pub/Sub queue config sets a 60 s visibility timeout and the
		// subscriber extends each message's deadline to it on receipt, so the
		// queue is provisioned with the same deadline.
		MinVisibilityTimeout: 60 * time.Second,
		MinMaxAttempts:       5,
		SampleEvery:          time.Second,
		Unobservable:         map[provider.Class]string{},
		ByDesign: map[string]string{
			"C12.1/no-extra-attempt-on-return": "Pub/Sub counts the next delivery of a nacked message as a new delivery attempt.",
		},
		Notes: []string{
			"No live in-flight count (Cloud Monitoring lags minutes): held-but-not-handled inside the client is judged on its consequences (redeliveries, duplicates, memory).",
			"Outpost's Pub/Sub client doesn't expose a message's delivery attempt: attempt checks use handler invocations and publish times.",
		},
	}
	if p.emulator != "" {
		c.Broker = "Pub/Sub emulator at " + p.emulator
		why := "the Pub/Sub emulator enforces no flow control and does not time leases like the service"
		c.Unobservable[provider.ClassFlowControl] = why
		c.Unobservable[provider.ClassLeaseTiming] = why
		c.Unobservable[provider.ClassCapacity] = "emulator throughput and latency say nothing about Pub/Sub"
	} else {
		c.Broker = "GCP Pub/Sub, project " + p.project
		c.Notes = append(c.Notes, "Dead-lettering needs the Pub/Sub service agent to have publisher on the dead-letter topic and subscriber on the subscription.")
	}
	return c
}

func (p *gcpProvider) infra(t *provider.Target) *mqinfra.MQInfraConfig {
	return &mqinfra.MQInfraConfig{
		GCPPubSub: &mqinfra.GCPPubSubInfraConfig{
			ProjectID:                 p.project,
			ServiceAccountCredentials: p.credJSON,
			TopicID:                   t.Spec.Name,
			SubscriptionID:            t.Spec.Name + "-sub",
			DLQTopicID:                t.Spec.Name + "-dlq",
			DLQSubscriptionID:         t.Spec.Name + "-dlq-sub",
		},
		Policy: mqinfra.Policy{
			VisibilityTimeout: int(t.Spec.VisibilityTimeout / time.Second),
			RetryLimit:        t.Spec.MaxAttempts - 1,
		},
	}
}

// Provision declares topic, subscription and dead-letter topic/subscription
// with Outpost's own infrastructure code.
func (p *gcpProvider) Provision(ctx context.Context, spec provider.QueueSpec) (*provider.Target, error) {
	t := &provider.Target{Spec: spec, Data: map[string]string{}}
	if err := mqinfra.New(p.infra(t)).Declare(ctx); err != nil {
		return nil, err
	}
	return t, nil
}

func (p *gcpProvider) Teardown(ctx context.Context, t *provider.Target) error {
	p.mu.Lock()
	delete(p.dlqs, t.Spec.Name)
	p.mu.Unlock()
	return mqinfra.New(p.infra(t)).TearDown(ctx)
}

// Sweep deletes subscriptions, then topics, made under prefix. Creation time
// comes from the resource name (see provider.ShouldSweep).
func (p *gcpProvider) Sweep(ctx context.Context, prefix string, olderThan time.Duration) (int, error) {
	n := 0
	subs := p.client.Subscriptions(ctx)
	for {
		s, err := subs.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return n, err
		}
		if provider.ShouldSweep(s.ID(), prefix, olderThan) {
			if err := s.Delete(ctx); err != nil {
				return n, err
			}
			n++
		}
	}
	topics := p.client.Topics(ctx)
	for {
		tp, err := topics.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil {
			return n, err
		}
		if provider.ShouldSweep(tp.ID(), prefix, olderThan) {
			if err := tp.Delete(ctx); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

// WorkerEnv is how an operator points Outpost's delivery queue at the topic.
func (p *gcpProvider) WorkerEnv(t *provider.Target) map[string]string {
	env := map[string]string{
		"GCP_PUBSUB_PROJECT":                   p.project,
		"GCP_PUBSUB_DELIVERY_TOPIC":            t.Spec.Name,
		"GCP_PUBSUB_DELIVERY_SUBSCRIPTION":     t.Spec.Name + "-sub",
		"GCP_PUBSUB_DELIVERY_DLQ_TOPIC":        t.Spec.Name + "-dlq",
		"GCP_PUBSUB_DELIVERY_DLQ_SUBSCRIPTION": t.Spec.Name + "-dlq-sub",
	}
	if p.emulator != "" {
		env["PUBSUB_EMULATOR_HOST"] = p.emulator
	}
	if p.credJSON != "" {
		env["GCP_PUBSUB_SERVICE_ACCOUNT_CREDENTIALS"] = p.credJSON
	}
	return env
}

// Sample: Pub/Sub has no live backlog or in-flight count.
func (p *gcpProvider) Sample(ctx context.Context, t *provider.Target) (provider.Sample, error) {
	return provider.Sample{At: time.Now(), Ready: -1, InFlight: -1, DLQ: -1}, nil
}

// ReadDLQ pulls and acks what the dead-letter subscription has now.
func (p *gcpProvider) ReadDLQ(ctx context.Context, t *provider.Target) ([][]byte, error) {
	p.mu.Lock()
	sub, ok := p.dlqs[t.Spec.Name]
	if !ok {
		sub = p.client.Subscription(t.Spec.Name + "-dlq-sub")
		sub.ReceiveSettings.MaxOutstandingMessages = 1000
		p.dlqs[t.Spec.Name] = sub
	}
	p.mu.Unlock()
	var (
		mu     sync.Mutex
		bodies [][]byte
	)
	rctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	err := sub.Receive(rctx, func(_ context.Context, m *pubsub.Message) {
		mu.Lock()
		bodies = append(bodies, m.Data)
		mu.Unlock()
		m.Ack()
	})
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return bodies, err
	}
	return bodies, nil
}

func (p *gcpProvider) Close() error {
	p.OutpostPublisher.Close()
	return p.client.Close()
}
