// Package profile loads run profiles: the sizes, limits, timeouts and
// thresholds a validation run uses. Profiles are data (YAML); every run writes
// the resolved profile into its output.
package profile

import (
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Duration is a time.Duration written as "10s" in YAML.
type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("line %d: %w", n.Line, err)
	}
	*d = Duration(v)
	return nil
}

func (d Duration) MarshalYAML() (any, error) { return time.Duration(d).String(), nil }
func (d Duration) MarshalJSON() ([]byte, error) {
	return []byte(`"` + time.Duration(d).String() + `"`), nil
}

// D returns d as a time.Duration.
func (d Duration) D() time.Duration { return time.Duration(d) }

// Profile is one run profile.
type Profile struct {
	Name string `yaml:"name" json:"name"`
	// Tier is "quick" (scaled-down sizes and timeouts, a capacity smoke step
	// without targets) or "official" (production-like sizes, the full
	// capacity envelope with targets). Both judge the same thresholds.
	Tier string `yaml:"tier" json:"tier"`
	// Cases to run: case ids, or "all" for every case in the tier.
	Cases []string `yaml:"cases" json:"cases"`
	// Parallel is how many cases run at once. Cases use their own queues;
	// capacity steps always run alone.
	Parallel int `yaml:"parallel" json:"parallel"`

	Queue struct {
		// VisibilityTimeout of test queues (raised to the provider's minimum).
		VisibilityTimeout Duration `yaml:"visibility_timeout" json:"visibility_timeout"`
		// MaxAttempts before dead-lettering (raised to the provider's minimum).
		MaxAttempts int `yaml:"max_attempts" json:"max_attempts"`
	} `yaml:"queue" json:"queue"`

	// GracePeriod after SIGTERM before a worker is killed, like an
	// orchestrator's termination grace period.
	GracePeriod Duration `yaml:"grace_period" json:"grace_period"`

	// Limits for the limit cases.
	Limits struct {
		Count int `yaml:"count" json:"count"`
	} `yaml:"limits" json:"limits"`

	Payloads struct {
		// Small is the normal message, e.g. json-2KB.
		Small string `yaml:"small" json:"small"`
		// Large is the large message for byte cases, e.g. rand-1MB; capped at
		// 90 % of the broker's max message size.
		Large string `yaml:"large" json:"large"`
	} `yaml:"payloads" json:"payloads"`

	Thresholds Thresholds `yaml:"thresholds" json:"thresholds"`

	Worker struct {
		// LogLevel of the worker's Outpost logger.
		LogLevel string `yaml:"log_level" json:"log_level"`
		// Env is extra environment for every worker (GOMEMLIMIT, GOGC, ...).
		Env map[string]string `yaml:"env" json:"env"`
		// CountEnv and BytesEnv are the Outpost settings for the delivery
		// queue's count and byte limits. A build without BytesEnv reports
		// byte cases NOT-IN-BUILD.
		CountEnv string `yaml:"count_env" json:"count_env"`
		BytesEnv string `yaml:"bytes_env" json:"bytes_env"`
	} `yaml:"worker" json:"worker"`

	Capacity Capacity `yaml:"capacity" json:"capacity"`
}

// Thresholds are the pass thresholds from the requirements.
type Thresholds struct {
	// WaitP99 and WaitMaxFraction bound how long a received message waits for
	// its handler (R4): p99 ≤ WaitP99, max ≤ fraction × visibility timeout.
	WaitP99         Duration `yaml:"wait_p99" json:"wait_p99"`
	WaitMaxFraction float64  `yaml:"wait_max_fraction" json:"wait_max_fraction"`
	// HeldBeyondLimit is how many messages a consumer may hold beyond what it
	// handles when at a limit (R5).
	HeldBeyondLimit int `yaml:"held_beyond_limit" json:"held_beyond_limit"`
	// HandBack is how soon messages a stopping consumer did not handle must
	// reach another consumer (R12).
	HandBack Duration `yaml:"hand_back" json:"hand_back"`
	// FirstAfterRestart bounds the first handled message after a restart (R13).
	FirstAfterRestart Duration `yaml:"first_after_restart" json:"first_after_restart"`
	// ErrorWithin is how long a permanent error may take to surface (R15).
	ErrorWithin Duration `yaml:"error_within" json:"error_within"`
}

// Capacity configures the capacity envelope.
type Capacity struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
	Limits  struct {
		Count int    `yaml:"count" json:"count"`
		Bytes string `yaml:"bytes" json:"bytes"`
	} `yaml:"limits" json:"limits"`
	Handler struct {
		Latency Duration `yaml:"latency" json:"latency"`
		Jitter  Duration `yaml:"jitter" json:"jitter"`
	} `yaml:"handler" json:"handler"`
	// Payload for the rate sweep, e.g. json-6KB.
	Payload string `yaml:"payload" json:"payload"`
	// RateSteps in messages per second, tried in order until one fails.
	RateSteps []int    `yaml:"rate_steps" json:"rate_steps"`
	Warmup    Duration `yaml:"warmup" json:"warmup"`
	StepHold  Duration `yaml:"step_hold" json:"step_hold"`
	Targets   struct {
		// MinRate the provider must sustain.
		MinRate int `yaml:"min_rate" json:"min_rate"`
		// Queue latency (publish → handler start) at a sustained step.
		QueueLatencyP50 Duration `yaml:"queue_latency_p50" json:"queue_latency_p50"`
		QueueLatencyP99 Duration `yaml:"queue_latency_p99" json:"queue_latency_p99"`
	} `yaml:"targets" json:"targets"`
	// Large payload drains: Count messages of Payload as a backlog.
	Large []struct {
		Payload string `yaml:"payload" json:"payload"`
		Count   int    `yaml:"count" json:"count"`
	} `yaml:"large" json:"large"`
}

// Load reads a profile by name from builtin (name.yaml) or, if name has a
// path separator or a .yaml suffix, from the file system.
func Load(name string, builtin fs.FS) (*Profile, error) {
	var (
		b   []byte
		err error
	)
	if strings.ContainsRune(name, os.PathSeparator) || strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
		b, err = os.ReadFile(name)
	} else {
		b, err = fs.ReadFile(builtin, name+".yaml")
	}
	if err != nil {
		return nil, fmt.Errorf("profile %q: %w", name, err)
	}
	p := Defaults()
	if err := yaml.Unmarshal(b, p); err != nil {
		return nil, fmt.Errorf("profile %q: %w", name, err)
	}
	if p.Tier != "quick" && p.Tier != "official" {
		return nil, fmt.Errorf("profile %q: tier must be quick or official", name)
	}
	return p, nil
}

// Defaults are the values a profile file does not set.
func Defaults() *Profile {
	p := &Profile{Tier: "quick", Cases: []string{"all"}, Parallel: 1}
	p.Queue.VisibilityTimeout = Duration(10 * time.Second)
	p.Queue.MaxAttempts = 5
	p.GracePeriod = Duration(30 * time.Second)
	p.Limits.Count = 10
	p.Payloads.Small = "json-2KB"
	p.Payloads.Large = "rand-1MB"
	p.Thresholds = Thresholds{
		WaitP99:           Duration(time.Second),
		WaitMaxFraction:   0.10,
		HeldBeyondLimit:   1,
		HandBack:          Duration(5 * time.Second),
		FirstAfterRestart: Duration(5 * time.Second),
		ErrorWithin:       Duration(90 * time.Second),
	}
	p.Worker.LogLevel = "warn"
	p.Worker.CountEnv = "DELIVERY_MAX_CONCURRENCY"
	p.Worker.BytesEnv = "DELIVERY_MAX_CONCURRENCY_BYTES"
	return p
}
