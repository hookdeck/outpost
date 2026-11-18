// Package provider defines what the queue consumer validation harness needs
// from a message broker, and the registry providers add themselves to.
//
// A provider knows the broker (its admin API and documented semantics) and how
// Outpost is configured to use it. It knows nothing about how the consumer
// under test receives, limits or prefetches messages: that is what the harness
// measures. See cmd/mqcheck/ADDING_A_PROVIDER.md.
package provider

import (
	"context"
	"errors"
	"time"
)

// ErrUnsupported is returned by optional Provider methods the broker or the
// provider cannot do. Cases that need them are reported N/A.
var ErrUnsupported = errors.New("unsupported by this provider")

// Provider is an opened provider, bound to one broker for one run.
type Provider interface {
	// Caps describes the broker as the harness needs to know it.
	Caps() Caps

	// Provision creates a queue with its dead-letter queue for one case and
	// returns how to reach it. Names start with spec.Name, which carries the
	// run's prefix, so Sweep can find leftovers.
	Provision(ctx context.Context, spec QueueSpec) (*Target, error)

	// Teardown deletes everything Provision created.
	Teardown(ctx context.Context, t *Target) error

	// Sweep deletes resources made under prefix (the bare prefix, as passed
	// to --prefix) that are older than olderThan; ShouldSweep decides. It
	// skips anything it can't judge, and returns how many it deleted.
	Sweep(ctx context.Context, prefix string, olderThan time.Duration) (int, error)

	// WorkerEnv returns the Outpost environment variables that point the
	// delivery queue consumer at t, as an operator would set them.
	WorkerEnv(t *Target) map[string]string

	// Publish sends message bodies to t. PublishWithOutpost covers most
	// brokers.
	Publish(ctx context.Context, t *Target, bodies [][]byte) error

	// Sample reads the broker's counters for t. Unknown values are -1.
	Sample(ctx context.Context, t *Target) (Sample, error)

	// ReadDLQ drains t's dead-letter queue and returns the bodies, or
	// ErrUnsupported.
	ReadDLQ(ctx context.Context, t *Target) ([][]byte, error)

	// Close releases the provider's clients.
	Close() error
}

// QueueSpec is the queue a case asks for.
type QueueSpec struct {
	// Name is unique per case and starts with the run's resource prefix.
	Name string
	// VisibilityTimeout is the broker's per-message timer (visibility
	// timeout, ack deadline, lock duration, AckWait).
	VisibilityTimeout time.Duration
	// MaxAttempts is the number of deliveries before a message is
	// dead-lettered.
	MaxAttempts int
}

// Target is a provisioned queue.
type Target struct {
	Spec QueueSpec
	// Data holds provider-specific identifiers (URLs, resource names).
	Data map[string]string
}

// Sample is one reading of the broker's counters. -1 means unknown.
type Sample struct {
	At time.Time `json:"at"`
	// Ready is the backlog: messages available to consumers.
	Ready int64 `json:"ready"`
	// InFlight is messages delivered to a consumer and not yet settled
	// (SQS not visible, RabbitMQ unacked, NATS ack pending, ...).
	InFlight int64 `json:"in_flight"`
	// DLQ is the dead-letter queue depth.
	DLQ int64 `json:"dlq"`
}

// Accuracy says how far an in-flight reading can be trusted.
type Accuracy string

const (
	// AccuracyExact is a live, exact count.
	AccuracyExact Accuracy = "exact"
	// AccuracyApproximate is live but approximate (real SQS).
	AccuracyApproximate Accuracy = "approximate"
	// AccuracyNone means the broker has no usable live count.
	AccuracyNone Accuracy = "none"
)

// Class is a kind of behavior a case relies on. A broker that cannot show it
// (an emulator that ignores flow control, say) lists it in Caps.Unobservable,
// and the harness reports those cases as not observable instead of judging
// them.
type Class string

const (
	// ClassFlowControl: the broker enforces delivery limits and its in-flight
	// state reflects what the consumer holds (held-but-not-handled, limits,
	// waiting under the visibility timeout).
	ClassFlowControl Class = "flow-control"
	// ClassLeaseTiming: the broker's visibility timeout expires and
	// redelivers as the real broker does.
	ClassLeaseTiming Class = "lease-timing"
	// ClassCapacity: throughput, latency and memory numbers mean something
	// for the real broker.
	ClassCapacity Class = "capacity"
	// ClassDeadLetter: the broker dead-letters after MaxAttempts.
	ClassDeadLetter Class = "dead-letter"
)

// Caps is what the harness needs to know about a broker. Everything here is
// documented broker behavior or a property of the setup (emulator vs real),
// never a property of Outpost's consumer.
type Caps struct {
	// Broker names the broker and how it runs, e.g. "LocalStack 3.8 (SQS API)".
	Broker string
	// InFlight says how Sample's InFlight can be trusted.
	InFlight Accuracy
	// MaxMessageBytes is the largest body the broker accepts. Payloads above
	// 90 % of it are scaled down and the report says so.
	MaxMessageBytes int
	// MinVisibilityTimeout is the smallest visibility timeout that matches how
	// Outpost runs on this broker; cases use max(profile, this).
	MinVisibilityTimeout time.Duration
	// MinMaxAttempts is the broker's smallest dead-letter threshold.
	MinMaxAttempts int
	// SampleEvery is how often Sample may be called.
	SampleEvery time.Duration
	// Unobservable lists behavior classes this setup cannot show, with why.
	Unobservable map[Class]string
	// CapacityNotJudged, when set, says why capacity numbers on this setup
	// are recorded but not judged against the profile's targets (e.g. an
	// emulated broker whose throughput is its own). Use Unobservable
	// [ClassCapacity] instead when the numbers mean nothing at all.
	CapacityNotJudged string
	// ByDesign maps "<case>/<check>" to the documented broker behavior that
	// makes that check fail whatever the consumer does. Such failures are
	// reported BY-DESIGN, not FAIL.
	ByDesign map[string]string
	// Notes are shown in the report under the provider.
	Notes []string
}

// Setting is one configuration value a provider reads from the environment.
type Setting struct {
	Env         string
	Description string
	Default     string
	Secret      bool
}

// Config is what Open gets.
type Config struct {
	// Prefix starts every resource name the provider creates.
	Prefix string
	// Get returns a setting's value: the environment variable, else the
	// setting's default.
	Get func(env string) string
}

// Registration is what a provider file registers.
type Registration struct {
	// Name is the value of --provider.
	Name string
	// Summary is one line for `mqcheck providers`.
	Summary string
	// Settings are the environment variables Open reads.
	Settings []Setting
	// PassEnv names environment variables the worker inherits from mqcheck's
	// own environment, for clients that read ambient credentials
	// (GOOGLE_APPLICATION_CREDENTIALS, AWS_PROFILE, ...).
	PassEnv []string
	// Open connects to the broker.
	Open func(ctx context.Context, cfg Config) (Provider, error)
	// Attempt, optional, runs in the worker process: it returns the broker's
	// delivery attempt count for a received message (1 = first delivery), or
	// 0 when unknown. queueMessage is the received mqs.Message's
	// QueueMessage: the queue client's own message type.
	Attempt func(queueMessage any) int
}
