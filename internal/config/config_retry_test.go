package config_test

import (
	"testing"
	"time"

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
			wantInterval: 30, // still have default even though not used
			wantMaxLimit: 7,  // overridden to schedule length
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

func TestGetRetryPollBackoff(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want time.Duration
	}{
		{
			name: "default is auto, 30s ceiling",
			yaml: "",
			want: 30 * time.Second,
		},
		{
			name: "auto follows a retry interval shorter than the ceiling",
			yaml: "retry_interval_seconds: 5\n",
			want: 5 * time.Second,
		},
		{
			name: "auto follows the shortest entry of a custom schedule",
			yaml: "retry_schedule: [10, 5, 300]\n",
			want: 5 * time.Second,
		},
		{
			name: "auto stays at the 30s ceiling under a longer schedule",
			yaml: "retry_schedule: [60, 300]\n",
			want: 30 * time.Second,
		},
		{
			name: "an explicit backoff below the auto value is used as-is",
			yaml: "retry_poll_backoff_ms: 100\n",
			want: 100 * time.Millisecond,
		},
		{
			name: "an explicit backoff longer than the shortest delay is honored, not capped",
			yaml: "retry_schedule: [5]\nretry_poll_backoff_ms: 10000\n",
			want: 10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOS := &mockOS{
				files:   map[string][]byte{"config.yaml": []byte(tt.yaml)},
				envVars: map[string]string{"CONFIG": "config.yaml"},
			}

			mockOS.envVars["API_KEY"] = "test-key"
			mockOS.envVars["API_JWT_SECRET"] = "test-jwt-secret"
			mockOS.envVars["AES_ENCRYPTION_SECRET"] = "test-aes-secret-16b"
			mockOS.envVars["POSTGRES_URL"] = "postgres://localhost:5432/test"
			mockOS.envVars["RABBITMQ_SERVER_URL"] = "amqp://localhost:5672"

			cfg, err := config.ParseWithOS(config.Flags{}, mockOS)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, cfg.GetRetryPollBackoff())
		})
	}
}

func TestRetryConfigurationValidation(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr string
	}{
		{
			name:    "zero retry_schedule entry rejected",
			yaml:    "retry_schedule: [5, 0, 300]\n",
			wantErr: "retry_schedule entries must be at least 1 second",
		},
		{
			name:    "negative retry_schedule entry rejected",
			yaml:    "retry_schedule: [-5, 300]\n",
			wantErr: "retry_schedule entries must be at least 1 second",
		},
		{
			name:    "zero retry_interval_seconds rejected",
			yaml:    "retry_interval_seconds: 0\n",
			wantErr: "retry_interval_seconds must be at least 1",
		},
		{
			name:    "negative retry_poll_backoff_ms rejected",
			yaml:    "retry_poll_backoff_ms: -1\n",
			wantErr: "retry_poll_backoff_ms must not be negative",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOS := &mockOS{
				files:   map[string][]byte{"config.yaml": []byte(tt.yaml)},
				envVars: map[string]string{"CONFIG": "config.yaml"},
			}

			mockOS.envVars["API_KEY"] = "test-key"
			mockOS.envVars["API_JWT_SECRET"] = "test-jwt-secret"
			mockOS.envVars["AES_ENCRYPTION_SECRET"] = "test-aes-secret-16b"
			mockOS.envVars["POSTGRES_URL"] = "postgres://localhost:5432/test"
			mockOS.envVars["RABBITMQ_SERVER_URL"] = "amqp://localhost:5672"

			_, err := config.ParseWithOS(config.Flags{}, mockOS)
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
