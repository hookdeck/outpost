package config

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/worker"
)

// SupervisorConfig picks the workers the supervisor restarts when they fail
// and sets how long it keeps restarting one before /healthz reports it failed.
// The limits apply to every worker in RestartWorkers.
type SupervisorConfig struct {
	RestartWorkers []string               `yaml:"restart_workers" env:"RESTART_WORKERS" envSeparator:"," desc:"Comma-separated list of workers the supervisor restarts when they fail instead of reporting them failed at once: publishmq, deliverymq, logmq, retrymq. Default: empty (no restarts)." required:"N"`
	Startup        SupervisorLimitsConfig `yaml:"startup" envPrefix:"STARTUP_"`
	Recovery       SupervisorLimitsConfig `yaml:"recovery" envPrefix:"RECOVERY_"`
}

type SupervisorLimitsConfig struct {
	MaxAttempts        int `yaml:"max_attempts" env:"MAX_ATTEMPTS" desc:"Restarts allowed before a failing worker is reported failed (503). -1 = no limit." required:"N"`
	MaxDurationSeconds int `yaml:"max_duration_seconds" env:"MAX_DURATION_SECONDS" desc:"Seconds from a worker's first failure: if no restart has succeeded by then, it is reported failed (503). Restarts are at most 60s apart. -1 = no limit." required:"N"`
}

// Names of the workers that support restarts, as used in restart_workers.
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

func (l SupervisorLimitsConfig) toLimits() worker.Limits {
	maxDuration := time.Duration(-1)
	if l.MaxDurationSeconds >= 0 {
		maxDuration = time.Duration(l.MaxDurationSeconds) * time.Second
	}
	return worker.Limits{MaxAttempts: l.MaxAttempts, MaxDuration: maxDuration}
}

// restartWorkers returns RestartWorkers with surrounding spaces trimmed and
// empty entries dropped.
func (c *SupervisorConfig) restartWorkers() []string {
	var names []string
	for _, name := range c.RestartWorkers {
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// RestartEnabled reports whether the worker named key is in RestartWorkers.
func (c *SupervisorConfig) RestartEnabled(key string) bool {
	return slices.Contains(c.restartWorkers(), key)
}

// RestartPolicy returns the restart policy and whether restarts are enabled
// for the worker named key, one of SupervisorWorkerKeys.
func (c *SupervisorConfig) RestartPolicy(key string) (worker.RestartPolicy, bool) {
	policy := worker.RestartPolicy{Startup: c.Startup.toLimits(), Recovery: c.Recovery.toLimits()}
	return policy, c.RestartEnabled(key)
}

func (c *SupervisorConfig) validate() error {
	for _, name := range c.restartWorkers() {
		if !slices.Contains(SupervisorWorkerKeys, name) {
			return fmt.Errorf("%w: supervisor.restart_workers: unknown worker %q, must be one of %s",
				ErrInvalidSupervisorWorker, name, strings.Join(SupervisorWorkerKeys, ", "))
		}
	}
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
	return checkLimits("supervisor.recovery", c.Recovery)
}
