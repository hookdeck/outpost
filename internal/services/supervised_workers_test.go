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
		name    string
		restart func(key string) []string
		want    bool
	}{
		{name: "default off", restart: func(string) []string { return nil }, want: false},
		{name: "listed", restart: func(key string) []string { return []string{key} }, want: true},
		{name: "all listed", restart: func(string) []string { return config.SupervisorWorkerKeys }, want: true},
		{name: "others listed", restart: func(key string) []string {
			var others []string
			for _, k := range config.SupervisorWorkerKeys {
				if k != key {
					others = append(others, k)
				}
			}
			return others
		}, want: false},
	}
	for _, key := range config.SupervisorWorkerKeys {
		for _, tc := range cases {
			t.Run(key+"/"+tc.name, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.InitDefaults()
				cfg.Supervisor.RestartWorkers = tc.restart(key)

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
}
