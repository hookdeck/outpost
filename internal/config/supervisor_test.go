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

func TestSupervisorConfig_PublishMQRestartPolicy(t *testing.T) {
	parse := func(t *testing.T, yamlBody string, env map[string]string) *config.Config {
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

	t.Run("defaults", func(t *testing.T) {
		cfg := parse(t, "", map[string]string{})
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: 5, MaxDuration: -1},
			Recovery: worker.Limits{MaxAttempts: -1, MaxDuration: 120 * time.Second},
		}, publishMQPolicy(t, cfg))
	})

	t.Run("env: global limits apply to publishmq", func(t *testing.T) {
		cfg := parse(t, "", map[string]string{
			"SUPERVISOR_STARTUP_MAX_ATTEMPTS":          "2",
			"SUPERVISOR_STARTUP_MAX_DURATION_SECONDS":  "10",
			"SUPERVISOR_RECOVERY_MAX_ATTEMPTS":         "7",
			"SUPERVISOR_RECOVERY_MAX_DURATION_SECONDS": "-1",
		})
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: 2, MaxDuration: 10 * time.Second},
			Recovery: worker.Limits{MaxAttempts: 7, MaxDuration: -1},
		}, publishMQPolicy(t, cfg))
	})

	t.Run("env: publishmq overrides; unset and empty inherit", func(t *testing.T) {
		cfg := parse(t, "", map[string]string{
			"SUPERVISOR_RECOVERY_MAX_DURATION_SECONDS":           "300",
			"SUPERVISOR_PUBLISHMQ_STARTUP_MAX_ATTEMPTS":          "-1",
			"SUPERVISOR_PUBLISHMQ_STARTUP_MAX_DURATION_SECONDS":  "",
			"SUPERVISOR_PUBLISHMQ_RECOVERY_MAX_DURATION_SECONDS": "-1",
		})
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: -1, MaxDuration: -1},
			Recovery: worker.Limits{MaxAttempts: -1, MaxDuration: -1},
		}, publishMQPolicy(t, cfg))
		assert.Equal(t, 300, cfg.Supervisor.Recovery.MaxDurationSeconds)
	})

	t.Run("yaml", func(t *testing.T) {
		cfg := parse(t, `
supervisor:
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
		assert.Equal(t, worker.RestartPolicy{
			Startup:  worker.Limits{MaxAttempts: 3, MaxDuration: 20 * time.Second},
			Recovery: worker.Limits{MaxAttempts: 4, MaxDuration: 120 * time.Second},
		}, publishMQPolicy(t, cfg))
	})

	t.Run("env: non-integer override is a parse error", func(t *testing.T) {
		m := &mockOS{files: map[string][]byte{}, envVars: map[string]string{
			"SUPERVISOR_PUBLISHMQ_STARTUP_MAX_ATTEMPTS": "five",
		}}
		_, err := config.ParseWithoutValidation(config.Flags{}, m)
		require.Error(t, err)
	})
}

func publishMQPolicy(t *testing.T, cfg *config.Config) worker.RestartPolicy {
	t.Helper()
	p, _ := cfg.Supervisor.RestartPolicy(config.SupervisorWorkerPublishMQ)
	return p
}

func TestSupervisorConfig_Enabled(t *testing.T) {
	parse := func(t *testing.T, yamlBody string, env map[string]string) bool {
		t.Helper()
		m := &mockOS{files: map[string][]byte{}, envVars: env}
		flags := config.Flags{}
		if yamlBody != "" {
			m.files["/c.yaml"] = []byte(yamlBody)
			flags.Config = "/c.yaml"
		}
		cfg, err := config.ParseWithoutValidation(flags, m)
		require.NoError(t, err)
		_, enabled := cfg.Supervisor.RestartPolicy(config.SupervisorWorkerPublishMQ)
		return enabled
	}

	assert.False(t, parse(t, "", map[string]string{}), "off by default")
	assert.True(t, parse(t, "", map[string]string{"SUPERVISOR_ENABLED": "true"}))
	assert.True(t, parse(t, "", map[string]string{"SUPERVISOR_ENABLED": "true", "SUPERVISOR_PUBLISHMQ_ENABLED": ""}), "empty inherits")
	assert.False(t, parse(t, "", map[string]string{"SUPERVISOR_ENABLED": "true", "SUPERVISOR_PUBLISHMQ_ENABLED": "false"}))
	assert.True(t, parse(t, "", map[string]string{"SUPERVISOR_PUBLISHMQ_ENABLED": "true"}))
	assert.True(t, parse(t, "supervisor:\n  workers:\n    publishmq:\n      enabled: true\n", map[string]string{}))
	assert.False(t, parse(t, "supervisor:\n  enabled: true\n  workers:\n    publishmq:\n      enabled: false\n", map[string]string{}))

	m := &mockOS{files: map[string][]byte{}, envVars: map[string]string{"SUPERVISOR_PUBLISHMQ_ENABLED": "yes please"}}
	_, err := config.ParseWithoutValidation(config.Flags{}, m)
	require.Error(t, err)
}

func TestSupervisorConfig_Validate(t *testing.T) {
	t.Run("-1 is valid", func(t *testing.T) {
		c := validConfig()
		c.Supervisor.Recovery.MaxDurationSeconds = -1
		c.Supervisor.Workers.PublishMQ.Startup.MaxAttempts = config.NewOptionalInt(-1)
		require.NoError(t, c.Validate(config.Flags{}))
	})
	t.Run("global below -1", func(t *testing.T) {
		c := validConfig()
		c.Supervisor.Startup.MaxAttempts = -2
		require.ErrorIs(t, c.Validate(config.Flags{}), config.ErrInvalidSupervisorLimit)
	})
	t.Run("override below -1", func(t *testing.T) {
		c := validConfig()
		c.Supervisor.Workers.PublishMQ.Recovery.MaxDurationSeconds = config.NewOptionalInt(-5)
		err := c.Validate(config.Flags{})
		require.ErrorIs(t, err, config.ErrInvalidSupervisorLimit)
		assert.Contains(t, err.Error(), "supervisor.workers.publishmq.recovery.max_duration_seconds")
	})
}

func TestSupervisorConfig_EachWorker(t *testing.T) {
	for _, key := range config.SupervisorWorkerKeys {
		t.Run(key, func(t *testing.T) {
			prefix := "SUPERVISOR_" + strings.ToUpper(key) + "_"
			m := &mockOS{files: map[string][]byte{}, envVars: map[string]string{
				"SUPERVISOR_RECOVERY_MAX_ATTEMPTS":       "9",
				prefix + "ENABLED":                       "true",
				prefix + "STARTUP_MAX_ATTEMPTS":          "1",
				prefix + "STARTUP_MAX_DURATION_SECONDS":  "2",
				prefix + "RECOVERY_MAX_DURATION_SECONDS": "-1",
			}}
			cfg, err := config.ParseWithoutValidation(config.Flags{}, m)
			require.NoError(t, err)

			policy, enabled := cfg.Supervisor.RestartPolicy(key)
			assert.True(t, enabled)
			assert.Equal(t, worker.RestartPolicy{
				Startup:  worker.Limits{MaxAttempts: 1, MaxDuration: 2 * time.Second},
				Recovery: worker.Limits{MaxAttempts: 9, MaxDuration: -1},
			}, policy)

			for _, other := range config.SupervisorWorkerKeys {
				if other == key {
					continue
				}
				p, on := cfg.Supervisor.RestartPolicy(other)
				assert.False(t, on, other)
				assert.Equal(t, 5, p.Startup.MaxAttempts, other)
			}

			yamlBody := "supervisor:\n  workers:\n    " + key + ":\n      enabled: true\n      recovery:\n        max_attempts: 4\n"
			m = &mockOS{files: map[string][]byte{"/c.yaml": []byte(yamlBody)}, envVars: map[string]string{}}
			cfg, err = config.ParseWithoutValidation(config.Flags{Config: "/c.yaml"}, m)
			require.NoError(t, err)
			policy, enabled = cfg.Supervisor.RestartPolicy(key)
			assert.True(t, enabled)
			assert.Equal(t, 4, policy.Recovery.MaxAttempts)

			c := validConfig()
			m = &mockOS{files: map[string][]byte{}, envVars: map[string]string{prefix + "RECOVERY_MAX_ATTEMPTS": "-3"}}
			parsed, err := config.ParseWithoutValidation(config.Flags{}, m)
			require.NoError(t, err)
			c.Supervisor = parsed.Supervisor
			err = c.Validate(config.Flags{})
			require.ErrorIs(t, err, config.ErrInvalidSupervisorLimit)
			assert.Contains(t, err.Error(), "supervisor.workers."+key+".recovery.max_attempts")
		})
	}
}
