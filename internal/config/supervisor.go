package config

import (
	"fmt"
	"time"

	"github.com/hookdeck/outpost/internal/worker"
)

// SupervisorConfig turns on restarting failed workers and sets how long the
// supervisor keeps restarting one before /healthz reports it failed. The
// top-level settings apply to every worker that supports restarts; Workers
// overrides them per worker.
type SupervisorConfig struct {
	Enabled  bool                    `yaml:"enabled" env:"ENABLED" desc:"If true, the supervisor restarts a failed worker (the publish queue consumer) instead of reporting it failed at once. Default: false." required:"N"`
	Startup  SupervisorLimitsConfig  `yaml:"startup" envPrefix:"STARTUP_"`
	Recovery SupervisorLimitsConfig  `yaml:"recovery" envPrefix:"RECOVERY_"`
	Workers  SupervisorWorkersConfig `yaml:"workers"`
}

type SupervisorLimitsConfig struct {
	MaxAttempts        int `yaml:"max_attempts" env:"MAX_ATTEMPTS" desc:"Restarts allowed before a failing worker is reported failed (503). -1 = no limit." required:"N"`
	MaxDurationSeconds int `yaml:"max_duration_seconds" env:"MAX_DURATION_SECONDS" desc:"Seconds from a worker's first failure: if no restart has succeeded by then, it is reported failed (503). Restarts are at most 60s apart. -1 = no limit." required:"N"`
}

type SupervisorWorkersConfig struct {
	PublishMQ  SupervisorWorkerConfig `yaml:"publishmq" envPrefix:"PUBLISHMQ_"`
	DeliveryMQ SupervisorWorkerConfig `yaml:"deliverymq" envPrefix:"DELIVERYMQ_"`
	LogMQ      SupervisorWorkerConfig `yaml:"logmq" envPrefix:"LOGMQ_"`
	RetryMQ    SupervisorWorkerConfig `yaml:"retrymq" envPrefix:"RETRYMQ_"`
}

// Supervisor worker keys, as used under supervisor.workers.
const (
	SupervisorWorkerPublishMQ  = "publishmq"
	SupervisorWorkerDeliveryMQ = "deliverymq"
	SupervisorWorkerLogMQ      = "logmq"
	SupervisorWorkerRetryMQ    = "retrymq"
)

// SupervisorWorkerKeys lists the workers that support restarts.
var SupervisorWorkerKeys = []string{
	SupervisorWorkerPublishMQ,
	SupervisorWorkerDeliveryMQ,
	SupervisorWorkerLogMQ,
	SupervisorWorkerRetryMQ,
}

func (c *SupervisorConfig) worker(key string) SupervisorWorkerConfig {
	switch key {
	case SupervisorWorkerPublishMQ:
		return c.Workers.PublishMQ
	case SupervisorWorkerDeliveryMQ:
		return c.Workers.DeliveryMQ
	case SupervisorWorkerLogMQ:
		return c.Workers.LogMQ
	case SupervisorWorkerRetryMQ:
		return c.Workers.RetryMQ
	}
	panic(fmt.Sprintf("unknown supervisor worker %q", key))
}

// SupervisorWorkerConfig overrides the global limits for one worker. Unset
// values inherit the global ones.
type SupervisorWorkerConfig struct {
	Enabled  OptionalBool                   `yaml:"enabled" env:"ENABLED" desc:"Overrides the global enabled for this worker. Unset inherits it." required:"N"`
	Startup  SupervisorLimitsOverrideConfig `yaml:"startup" envPrefix:"STARTUP_"`
	Recovery SupervisorLimitsOverrideConfig `yaml:"recovery" envPrefix:"RECOVERY_"`
}

type SupervisorLimitsOverrideConfig struct {
	MaxAttempts        OptionalInt `yaml:"max_attempts" env:"MAX_ATTEMPTS" desc:"Overrides the global max_attempts for this worker. Unset inherits it." required:"N"`
	MaxDurationSeconds OptionalInt `yaml:"max_duration_seconds" env:"MAX_DURATION_SECONDS" desc:"Overrides the global max_duration_seconds for this worker. Unset inherits it." required:"N"`
}

func (o SupervisorLimitsOverrideConfig) resolve(global SupervisorLimitsConfig) SupervisorLimitsConfig {
	return SupervisorLimitsConfig{
		MaxAttempts:        o.MaxAttempts.Or(global.MaxAttempts),
		MaxDurationSeconds: o.MaxDurationSeconds.Or(global.MaxDurationSeconds),
	}
}

func (l SupervisorLimitsConfig) toLimits() worker.Limits {
	maxDuration := time.Duration(-1)
	if l.MaxDurationSeconds >= 0 {
		maxDuration = time.Duration(l.MaxDurationSeconds) * time.Second
	}
	return worker.Limits{MaxAttempts: l.MaxAttempts, MaxDuration: maxDuration}
}

// effective returns a worker's limits with unset overrides filled from the globals.
func (c *SupervisorConfig) effective(w SupervisorWorkerConfig) (startup, recovery SupervisorLimitsConfig) {
	return w.Startup.resolve(c.Startup), w.Recovery.resolve(c.Recovery)
}

// Effective returns a worker's settings with unset overrides filled from the
// globals.
func (c *SupervisorConfig) Effective(key string) (enabled bool, startup, recovery SupervisorLimitsConfig) {
	w := c.worker(key)
	startup, recovery = c.effective(w)
	return w.Enabled.Or(c.Enabled), startup, recovery
}

// RestartPolicy returns a worker's restart policy and whether restarts are
// enabled for it. key is one of SupervisorWorkerKeys.
func (c *SupervisorConfig) RestartPolicy(key string) (worker.RestartPolicy, bool) {
	enabled, startup, recovery := c.Effective(key)
	return worker.RestartPolicy{Startup: startup.toLimits(), Recovery: recovery.toLimits()}, enabled
}

func (c *SupervisorConfig) validate() error {
	check := func(path string, v int) error {
		if v < -1 {
			return fmt.Errorf("%w: %s must be >= -1, got %d", ErrInvalidSupervisorLimit, path, v)
		}
		return nil
	}
	checkLimits := func(prefix string, l SupervisorLimitsConfig) error {
		if err := check(prefix+".max_attempts", l.MaxAttempts); err != nil {
			return err
		}
		return check(prefix+".max_duration_seconds", l.MaxDurationSeconds)
	}
	if err := checkLimits("supervisor.startup", c.Startup); err != nil {
		return err
	}
	if err := checkLimits("supervisor.recovery", c.Recovery); err != nil {
		return err
	}
	for _, key := range SupervisorWorkerKeys {
		_, startup, recovery := c.Effective(key)
		if err := checkLimits("supervisor.workers."+key+".startup", startup); err != nil {
			return err
		}
		if err := checkLimits("supervisor.workers."+key+".recovery", recovery); err != nil {
			return err
		}
	}
	return nil
}
