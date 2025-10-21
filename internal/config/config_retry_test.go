package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestRetrySchedule(t *testing.T) {
	tests := []struct {
		name         string
		files        map[string][]byte
		envVars      map[string]string
		wantSchedule []int
		wantInterval int
		wantMaxLimit int
	}{
		{
			name:         "default empty retry schedule",
			files:        map[string][]byte{},
			envVars:      map[string]string{},
			wantSchedule: []int{},
			wantInterval: 30, // default exponential backoff interval
			wantMaxLimit: 10, // default max limit
		},
		{
			name: "yaml retry schedule overrides max limit",
			files: map[string][]byte{
				"config.yaml": []byte(`
retry_schedule: [5, 300, 1800, 7200, 18000, 36000, 36000]
`),
			},
			envVars: map[string]string{
				"CONFIG": "config.yaml",
			},
			wantSchedule: []int{5, 300, 1800, 7200, 18000, 36000, 36000},
			wantInterval: 30,         // still have default even though not used
			wantMaxLimit: 7,          // overridden to schedule length
		},
		{
			name: "env retry schedule overrides yaml and max limit",
			files: map[string][]byte{
				"config.yaml": []byte(`
retry_schedule: [10, 20, 30]
`),
			},
			envVars: map[string]string{
				"CONFIG":         "config.yaml",
				"RETRY_SCHEDULE": "5,300,1800",
			},
			wantSchedule: []int{5, 300, 1800},
			wantInterval: 30,
			wantMaxLimit: 3, // overridden to env schedule length
		},
		{
			name: "retry_interval_seconds without retry_schedule",
			files: map[string][]byte{
				"config.yaml": []byte(`
retry_interval_seconds: 60
`),
			},
			envVars: map[string]string{
				"CONFIG": "config.yaml",
			},
			wantSchedule: []int{},
			wantInterval: 60,
			wantMaxLimit: 10, // default max limit used
		},
		{
			name: "both retry_schedule and retry_interval_seconds set",
			files: map[string][]byte{
				"config.yaml": []byte(`
retry_schedule: [5, 300, 1800]
retry_interval_seconds: 60
`),
			},
			envVars: map[string]string{
				"CONFIG": "config.yaml",
			},
			wantSchedule: []int{5, 300, 1800},
			wantInterval: 60, // both present, schedule takes precedence
			wantMaxLimit: 3,  // overridden to schedule length
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOS := &mockOS{
				files:   tt.files,
				envVars: tt.envVars,
			}

			mockOS.envVars["API_KEY"] = "test-key"
			mockOS.envVars["API_JWT_SECRET"] = "test-jwt-secret"
			mockOS.envVars["AES_ENCRYPTION_SECRET"] = "test-aes-secret-16b"
			mockOS.envVars["POSTGRES_URL"] = "postgres://localhost:5432/test"
			mockOS.envVars["RABBITMQ_SERVER_URL"] = "amqp://localhost:5672"

			cfg, err := config.ParseWithOS(config.Flags{}, mockOS)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantSchedule, cfg.RetrySchedule)
			assert.Equal(t, tt.wantInterval, cfg.RetryIntervalSeconds)
			assert.Equal(t, tt.wantMaxLimit, cfg.RetryMaxLimit)
		})
	}
}
