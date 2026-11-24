package config_test

import (
	"strings"
	"testing"
	"time"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseSupervisor(t *testing.T, yamlBody string, env map[string]string) *config.Config {
	t.Helper()
	m := &mockOS{files: map[string][]byte{}, envVars: env}
	flags := config.Flags{}
	if yamlBody != "" {
		m.files["/c.yaml"] = []byte(yamlBody)
		flags.Config = "/c.yaml"
	}
	cfg, err := config.ParseWithoutValidation(flags, m)
	require.NoError(t, err)
	return cfg
}

func enabledWorkers(cfg *config.Config) []string {
	var on []string
	for _, key := range config.SupervisorWorkerKeys {
		if _, enabled := cfg.Supervisor.RestartPolicy(key); enabled {
			on = append(on, key)
		}
	}
	return on
}

func TestSupervisorConfig_RestartPolicy(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg := parseSupervisor(t, "", map[string]string{})
		for _, key := range config.SupervisorWorkerKeys {
			p, _ := cfg.Supervisor.RestartPolicy(key)
			assert.Equal(t, worker.RestartPolicy{
				Startup:  worker.Limits{MaxAttempts: 5, MaxDuration: -1},
				Recovery: worker.Limits{MaxAttempts: -1, MaxDuration: 120 * time.Second},
			}, p, key)
		}
	})

	t.Run("env: limits apply to every worker", func(t *testing.T) {
		cfg := parseSupervisor(t, "", map[string]string{
			"SUPERVISOR_RESTART_WORKERS":               "publishmq,retrymq",
			"SUPERVISOR_STARTUP_MAX_ATTEMPTS":          "2",
			"SUPERVISOR_STARTUP_MAX_DURATION_SECONDS":  "10",
			"SUPERVISOR_RECOVERY_MAX_ATTEMPTS":         "7",
			"SUPERVISOR_RECOVERY_MAX_DURATION_SECONDS": "-1",
		})
		for _, key := range config.SupervisorWorkerKeys {
			p, _ := cfg.Supervisor.RestartPolicy(key)
			assert.Equal(t, worker.RestartPolicy{
				Startup:  worker.Limits{MaxAttempts: 2, MaxDuration: 10 * time.Second},
				Recovery: worker.Limits{MaxAttempts: 7, MaxDuration: -1},
			}, p, key)
		}
	})

	t.Run("yaml", func(t *testing.T) {
		cfg := parseSupervisor(t, `
supervisor:
  startup:
    max_attempts: 3
  recovery:
    max_duration_seconds: 300
`, map[string]string{})
		p, _ := cfg.Supervisor.RestartPolicy(config.SupervisorWorkerPublishMQ)
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: 3, MaxDuration: -1},
			Recovery: worker.Limits{MaxAttempts: -1, MaxDuration: 300 * time.Second},
		}, p)
	})
}

func TestSupervisorConfig_Overrides(t *testing.T) {
	t.Run("env: override one worker; unset and empty inherit", func(t *testing.T) {
		cfg := parseSupervisor(t, "", map[string]string{
			"SUPERVISOR_RESTART_WORKERS":                         "publishmq,deliverymq",
			"SUPERVISOR_RECOVERY_MAX_DURATION_SECONDS":           "300",
			"SUPERVISOR_PUBLISHMQ_STARTUP_MAX_ATTEMPTS":          "-1",
			"SUPERVISOR_PUBLISHMQ_STARTUP_MAX_DURATION_SECONDS":  "",
			"SUPERVISOR_PUBLISHMQ_RECOVERY_MAX_DURATION_SECONDS": "-1",
		})
		p, on := cfg.Supervisor.RestartPolicy(config.SupervisorWorkerPublishMQ)
		assert.True(t, on)
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: -1, MaxDuration: -1},
			Recovery: worker.Limits{MaxAttempts: -1, MaxDuration: -1},
		}, p)

		p, on = cfg.Supervisor.RestartPolicy(config.SupervisorWorkerDeliveryMQ)
		assert.True(t, on)
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: worker.Limits{MaxAttempts: -1, MaxDuration: 300 * time.Second},
		}, p, "other workers keep the globals")
	})

	t.Run("yaml", func(t *testing.T) {
		cfg := parseSupervisor(t, `
supervisor:
  restart_workers: [publishmq]
  startup:
    max_attempts: 3
  workers:
    publishmq:
      startup:
        max_duration_seconds: 20
      recovery:
        max_attempts: 4
        max_duration_seconds:
`, map[string]string{})
		p, _ := cfg.Supervisor.RestartPolicy(config.SupervisorWorkerPublishMQ)
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: 3, MaxDuration: 20 * time.Second},
			Recovery: worker.Limits{MaxAttempts: 4, MaxDuration: 120 * time.Second},
		}, p)
	})

	t.Run("overrides for a worker not in the list are ignored", func(t *testing.T) {
		cfg := parseSupervisor(t, "", map[string]string{
			"SUPERVISOR_RESTART_WORKERS":              "logmq",
			"SUPERVISOR_RETRYMQ_STARTUP_MAX_ATTEMPTS": "1",
		})
		assert.Equal(t, []string{"logmq"}, enabledWorkers(cfg))
		c := validConfig()
		c.Supervisor = cfg.Supervisor
		require.NoError(t, c.Validate(config.Flags{}))
	})

	t.Run("env: non-integer override is a parse error", func(t *testing.T) {
		m := &mockOS{files: map[string][]byte{}, envVars: map[string]string{
			"SUPERVISOR_PUBLISHMQ_STARTUP_MAX_ATTEMPTS": "five",
		}}
		_, err := config.ParseWithoutValidation(config.Flags{}, m)
		require.Error(t, err)
	})

	for _, key := range config.SupervisorWorkerKeys {
		t.Run("each worker/"+key, func(t *testing.T) {
			prefix := "SUPERVISOR_" + strings.ToUpper(key) + "_"
			cfg := parseSupervisor(t, "", map[string]string{
				"SUPERVISOR_RESTART_WORKERS":             strings.Join(config.SupervisorWorkerKeys, ","),
				"SUPERVISOR_RECOVERY_MAX_ATTEMPTS":       "9",
				prefix + "STARTUP_MAX_ATTEMPTS":          "1",
				prefix + "STARTUP_MAX_DURATION_SECONDS":  "2",
				prefix + "RECOVERY_MAX_DURATION_SECONDS": "-1",
			})
			p, _ := cfg.Supervisor.RestartPolicy(key)
			assert.Equal(t, worker.RestartPolicy{
				Startup:  worker.Limits{MaxAttempts: 1, MaxDuration: 2 * time.Second},
				Recovery: worker.Limits{MaxAttempts: 9, MaxDuration: -1},
			}, p)
			for _, other := range config.SupervisorWorkerKeys {
				if other == key {
					continue
				}
				op, _ := cfg.Supervisor.RestartPolicy(other)
				assert.Equal(t, 5, op.Startup.MaxAttempts, other)
				assert.Equal(t, 120*time.Second, op.Recovery.MaxDuration, other)
			}

			cfg = parseSupervisor(t, "supervisor:\n  workers:\n    "+key+":\n      recovery:\n        max_attempts: 4\n", map[string]string{})
			p, _ = cfg.Supervisor.RestartPolicy(key)
			assert.Equal(t, 4, p.Recovery.MaxAttempts)

			parsed := parseSupervisor(t, "", map[string]string{prefix + "RECOVERY_MAX_ATTEMPTS": "-3"})
			c := validConfig()
			c.Supervisor = parsed.Supervisor
			err := c.Validate(config.Flags{})
			require.ErrorIs(t, err, config.ErrInvalidSupervisorLimit)
			assert.Contains(t, err.Error(), "supervisor.workers."+key+".recovery.max_attempts")
		})
	}
}

func TestSupervisorConfig_RestartWorkers(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		env  map[string]string
		want []string
	}{
		{name: "default: none", want: nil},
		{name: "env: empty", env: map[string]string{"SUPERVISOR_RESTART_WORKERS": ""}, want: nil},
		{name: "env: one", env: map[string]string{"SUPERVISOR_RESTART_WORKERS": "deliverymq"}, want: []string{"deliverymq"}},
		{
			name: "env: all, spaces and empty entries ignored",
			env:  map[string]string{"SUPERVISOR_RESTART_WORKERS": " publishmq, deliverymq,,logmq ,retrymq,"},
			want: config.SupervisorWorkerKeys,
		},
		{name: "env: duplicates harmless", env: map[string]string{"SUPERVISOR_RESTART_WORKERS": "logmq,logmq"}, want: []string{"logmq"}},
		{name: "yaml: list", yaml: "supervisor:\n  restart_workers: [publishmq, retrymq]\n", want: []string{"publishmq", "retrymq"}},
		{name: "yaml: empty list", yaml: "supervisor:\n  restart_workers: []\n", want: nil},
		{
			name: "env wins over yaml",
			yaml: "supervisor:\n  restart_workers:\n    - publishmq\n",
			env:  map[string]string{"SUPERVISOR_RESTART_WORKERS": "logmq"},
			want: []string{"logmq"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := tc.env
			if env == nil {
				env = map[string]string{}
			}
			cfg := parseSupervisor(t, tc.yaml, env)
			assert.Equal(t, tc.want, enabledWorkers(cfg))

			c := validConfig()
			c.Supervisor = cfg.Supervisor
			require.NoError(t, c.Validate(config.Flags{}))
		})
	}
}

func TestSupervisorConfig_Validate(t *testing.T) {
	t.Run("-1 is valid", func(t *testing.T) {
		c := validConfig()
		c.Supervisor.Recovery.MaxDurationSeconds = -1
		c.Supervisor.Startup.MaxAttempts = -1
		require.NoError(t, c.Validate(config.Flags{}))
	})
	t.Run("startup below -1", func(t *testing.T) {
		c := validConfig()
		c.Supervisor.Startup.MaxAttempts = -2
		err := c.Validate(config.Flags{})
		require.ErrorIs(t, err, config.ErrInvalidSupervisorLimit)
		assert.Contains(t, err.Error(), "supervisor.startup.max_attempts")
	})
	t.Run("recovery below -1", func(t *testing.T) {
		c := validConfig()
		c.Supervisor.Recovery.MaxDurationSeconds = -5
		err := c.Validate(config.Flags{})
		require.ErrorIs(t, err, config.ErrInvalidSupervisorLimit)
		assert.Contains(t, err.Error(), "supervisor.recovery.max_duration_seconds")
	})
	t.Run("unknown restart worker", func(t *testing.T) {
		for _, bad := range []string{"http-server", "PUBLISHMQ", "publishmq-consumer"} {
			c := validConfig()
			c.Supervisor.RestartWorkers = []string{"publishmq", bad}
			err := c.Validate(config.Flags{})
			require.ErrorIs(t, err, config.ErrInvalidSupervisorWorker, bad)
			assert.Contains(t, err.Error(), `"`+bad+`"`)
		}
	})
}
