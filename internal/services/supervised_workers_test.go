package services_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/services"
	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/stretchr/testify/assert"
)

func TestSupervisedWorkers_Enabled(t *testing.T) {
	cases := []struct {
		name     string
		global   bool
		override *bool
		want     bool
	}{
		{name: "default off", want: false},
		{name: "global on", global: true, want: true},
		{name: "global on, worker off", global: true, override: new(false), want: false},
		{name: "global off, worker on", override: new(true), want: true},
	}
	setOverride := func(cfg *config.Config, key string, v bool) {
		o := config.NewOptionalBool(v)
		switch key {
		case config.SupervisorWorkerPublishMQ:
			cfg.Supervisor.Workers.PublishMQ.Enabled = o
		case config.SupervisorWorkerDeliveryMQ:
			cfg.Supervisor.Workers.DeliveryMQ.Enabled = o
		case config.SupervisorWorkerLogMQ:
			cfg.Supervisor.Workers.LogMQ.Enabled = o
		case config.SupervisorWorkerRetryMQ:
			cfg.Supervisor.Workers.RetryMQ.Enabled = o
		}
	}
	for _, key := range config.SupervisorWorkerKeys {
		for _, tc := range cases {
			t.Run(key+"/"+tc.name, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.InitDefaults()
				cfg.Supervisor.Enabled = tc.global
				if tc.override != nil {
					setOverride(cfg, key, *tc.override)
				}

				enabled, opts := services.RestartOptions(cfg, key)
				assert.Equal(t, tc.want, enabled)
				if tc.want {
					assert.Len(t, opts, 1)
				} else {
					assert.Empty(t, opts)
				}

				if key == config.SupervisorWorkerRetryMQ {
					return
				}
				q := mqs.NewInMemoryQueue(nil)
				w, wOpts := services.NewSupervisedConsumerWorker(cfg, key, key+"-consumer", q.Subscribe, handlerFunc(nil), 1, testutil.CreateTestLogger(t))
				assert.Equal(t, key+"-consumer", w.Name())
				assert.Len(t, wOpts, len(opts))
			})
		}
	}

	// Enabling one worker leaves the others off.
	cfg := &config.Config{}
	cfg.InitDefaults()
	cfg.Supervisor.Workers.LogMQ.Enabled = config.NewOptionalBool(true)
	for _, key := range config.SupervisorWorkerKeys {
		enabled, _ := services.RestartOptions(cfg, key)
		assert.Equal(t, key == config.SupervisorWorkerLogMQ, enabled, key)
	}
}
