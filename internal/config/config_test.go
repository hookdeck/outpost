package config_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
)

type mockOS struct {
	files    map[string][]byte
	envVars  map[string]string
	statErrs map[string]error
}

func (m *mockOS) Getenv(key string) string {
	return m.envVars[key]
}

func (m *mockOS) Stat(name string) (os.FileInfo, error) {
	if err, ok := m.statErrs[name]; ok {
		return nil, err
	}
	if _, ok := m.files[name]; !ok {
		return nil, os.ErrNotExist
	}
	return nil, nil
}

func (m *mockOS) ReadFile(name string) ([]byte, error) {
	if data, ok := m.files[name]; ok {
		return data, nil
	}
	return nil, os.ErrNotExist
}

func (m *mockOS) Environ() []string {
	var env []string
	for k, v := range m.envVars {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	return env
}

func TestDefaultValues(t *testing.T) {
	mockOS := &mockOS{
		files:   make(map[string][]byte),
		envVars: make(map[string]string),
	}

	cfg, err := config.ParseWithoutValidation(config.Flags{}, mockOS)
	assert.NoError(t, err)

	// Test only fields that have explicit defaults
	assert.Equal(t, 3333, cfg.APIPort)
	assert.Equal(t, "127.0.0.1", cfg.Redis.Host)
	assert.Equal(t, 6379, cfg.Redis.Port)
	assert.Equal(t, "outpost", cfg.MQs.RabbitMQ.Exchange)
	assert.Equal(t, "outpost-delivery", cfg.MQs.RabbitMQ.DeliveryQueue)
	assert.Equal(t, "outpost-log", cfg.MQs.RabbitMQ.LogQueue)
	assert.Equal(t, 5, cfg.MQs.DeliveryRetryLimit)
	assert.Equal(t, 5, cfg.MQs.LogRetryLimit)
	assert.Equal(t, 1, cfg.PublishMaxConcurrency)
	assert.Equal(t, 1, cfg.DeliveryMaxConcurrency)
	assert.Equal(t, 1, cfg.LogMaxConcurrency)
	assert.Equal(t, 30, cfg.RetryIntervalSeconds)
	assert.Equal(t, 10, cfg.RetryMaxLimit)
	assert.Equal(t, 20, cfg.MaxDestinationsPerTenant)
	assert.Equal(t, 5, cfg.DeliveryTimeoutSeconds)
	assert.Equal(t, "config/outpost/destinations", cfg.Destinations.MetadataPath)
	assert.Equal(t, 10, cfg.LogBatchThresholdSeconds)
	assert.Equal(t, 1000, cfg.LogBatchSize)
	assert.Equal(t, "x-outpost-", cfg.Destinations.Webhook.HeaderPrefix)
}

func TestYAMLConfig(t *testing.T) {
	mockOS := &mockOS{
		files: map[string][]byte{
			"config.yaml": []byte(`
api_port: 9090
redis:
  host: custom.redis.local
  password: custom_secret
  database: 1
clickhouse:
  addr: localhost:9000
  username: default
  password: secret
  database: default
topics:
  - topic1
  - topic2
mqs:
  rabbitmq:
    exchange: custom-outpost
    delivery_queue: custom-delivery
    log_queue: custom-log
  delivery_retry_limit: 3
  log_retry_limit: 3
publish_max_concurrency: 5
delivery_max_concurrency: 5
log_max_concurrency: 5
retry_interval_seconds: 60
max_destinations_per_tenant: 50
delivery_timeout_seconds: 10
aes_encryption_secret: test-secret
`),
		},
		envVars: map[string]string{
			"CONFIG": "config.yaml",
		},
	}

	cfg, err := config.ParseWithoutValidation(config.Flags{}, mockOS)
	assert.NoError(t, err)

	assert.Equal(t, 9090, cfg.APIPort)
	assert.Equal(t, "custom.redis.local", cfg.Redis.Host)
	assert.Equal(t, "custom_secret", cfg.Redis.Password)
	assert.Equal(t, 1, cfg.Redis.Database)
	assert.Equal(t, []string{"topic1", "topic2"}, cfg.Topics)
	assert.Equal(t, "custom-outpost", cfg.MQs.RabbitMQ.Exchange)
	assert.Equal(t, "custom-delivery", cfg.MQs.RabbitMQ.DeliveryQueue)
	assert.Equal(t, "custom-log", cfg.MQs.RabbitMQ.LogQueue)
	assert.Equal(t, 3, cfg.MQs.DeliveryRetryLimit)
	assert.Equal(t, 3, cfg.MQs.LogRetryLimit)
	assert.Equal(t, 5, cfg.PublishMaxConcurrency)
	assert.Equal(t, 5, cfg.DeliveryMaxConcurrency)
	assert.Equal(t, 5, cfg.LogMaxConcurrency)
	assert.Equal(t, 60, cfg.RetryIntervalSeconds)
	assert.Equal(t, 50, cfg.MaxDestinationsPerTenant)
	assert.Equal(t, 10, cfg.DeliveryTimeoutSeconds)
	assert.Equal(t, "test-secret", cfg.AESEncryptionSecret)
	assert.Equal(t, "localhost:9000", cfg.ClickHouse.Addr)
	assert.Equal(t, "default", cfg.ClickHouse.Username)
	assert.Equal(t, "secret", cfg.ClickHouse.Password)
	assert.Equal(t, "default", cfg.ClickHouse.Database)
}

func TestConfigFileResolution(t *testing.T) {
	tests := []struct {
		name     string
		flagPath string
		envPath  string
		files    map[string][]byte
		want     *config.Config
		wantErr  bool
	}{
		{
			name:     "flag path takes precedence when same as env",
			flagPath: "config.yaml",
			envPath:  "config.yaml",
			files: map[string][]byte{
				"config.yaml": []byte(`api_port: 8080`),
			},
			want:    &config.Config{APIPort: 8080},
			wantErr: false,
		},
		{
			name:     "error when flag and env paths conflict",
			flagPath: "flag.yaml",
			envPath:  "env.yaml",
			files: map[string][]byte{
				"flag.yaml": []byte(`api_port: 8080`),
				"env.yaml":  []byte(`api_port: 9090`),
			},
			want:    nil,
			wantErr: true,
		},
		{
			name:    "env path used when flag not provided",
			envPath: "env.yaml",
			files: map[string][]byte{
				"env.yaml": []byte(`api_port: 9090`),
			},
			want:    &config.Config{APIPort: 9090},
			wantErr: false,
		},
		{
			name: "default locations checked in order",
			files: map[string][]byte{
				"config/outpost/config.yaml": []byte(`api_port: 7070`),
			},
			want:    &config.Config{APIPort: 7070},
			wantErr: false,
		},
		{
			name:    "no error when no config found",
			want:    &config.Config{APIPort: 3333}, // Default port value
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOS := &mockOS{
				files:   tt.files,
				envVars: map[string]string{"CONFIG": tt.envPath},
			}

			cfg, err := config.ParseWithoutValidation(config.Flags{Config: tt.flagPath}, mockOS)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseWithoutValidation() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && cfg != nil {
				assert.Equal(t, tt.want.APIPort, cfg.APIPort)
			}
		})
	}
}

func TestConfigPrecedence(t *testing.T) {
	const (
		defaultPort = 3333 // from initDefaults
		configPort  = 8080 // value in config file
		envPort     = 9090 // value from environment variable
	)

	// Helper variables for string/byte representations
	configYAML := fmt.Sprintf(`api_port: %d`, configPort)
	envPortStr := fmt.Sprintf("%d", envPort)

	tests := []struct {
		name     string
		files    map[string][]byte
		envVars  map[string]string
		wantPort int
	}{
		{
			name:     "default value when nothing set",
			files:    map[string][]byte{},
			envVars:  map[string]string{},
			wantPort: defaultPort,
		},
		{
			name: "config file overrides default",
			files: map[string][]byte{
				"config.yaml": []byte(configYAML),
			},
			envVars: map[string]string{
				"CONFIG": "config.yaml",
			},
			wantPort: configPort,
		},
		{
			name: "env var overrides config file",
			files: map[string][]byte{
				"config.yaml": []byte(configYAML),
			},
			envVars: map[string]string{
				"CONFIG":   "config.yaml",
				"API_PORT": envPortStr,
			},
			wantPort: envPort,
		},
		{
			name:  "env var overrides default when no config file",
			files: map[string][]byte{},
			envVars: map[string]string{
				"API_PORT": envPortStr,
			},
			wantPort: envPort,
		},
		{
			name: "empty config file doesn't override default",
			files: map[string][]byte{
				"config.yaml": []byte(``),
			},
			envVars: map[string]string{
				"CONFIG": "config.yaml",
			},
			wantPort: defaultPort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOS := &mockOS{
				files:   tt.files,
				envVars: tt.envVars,
			}

			cfg, err := config.ParseWithoutValidation(config.Flags{}, mockOS)
			assert.NoError(t, err)
			assert.Equal(t, tt.wantPort, cfg.APIPort)
		})
	}
}

func TestMixedYAMLAndEnvConfig(t *testing.T) {
	yamlConfig := `
mqs:
  delivery_retry_limit: 10
  log_retry_limit: 15
  rabbitmq:
    exchange: custom-exchange
`
	mockOS := &mockOS{
		files: map[string][]byte{
			"config.yaml": []byte(yamlConfig),
		},
		envVars: map[string]string{
			"CONFIG":                  "config.yaml",
			"RABBITMQ_SERVER_URL":     "amqp://user:pass@host:5672",
			"RABBITMQ_DELIVERY_QUEUE": "env-delivery-queue",
			"RABBITMQ_EXCHANGE":       "env-exchange",
		},
	}

	cfg, err := config.ParseWithoutValidation(config.Flags{}, mockOS)
	assert.NoError(t, err)

	// Values from YAML
	assert.Equal(t, 10, cfg.MQs.DeliveryRetryLimit)
	assert.Equal(t, 15, cfg.MQs.LogRetryLimit)

	// Values from env should override YAML
	assert.Equal(t, "env-exchange", cfg.MQs.RabbitMQ.Exchange)
	assert.Equal(t, "amqp://user:pass@host:5672", cfg.MQs.RabbitMQ.ServerURL)
	assert.Equal(t, "env-delivery-queue", cfg.MQs.RabbitMQ.DeliveryQueue)

	// Default values should still be present for unset fields
	assert.Equal(t, "outpost-log", cfg.MQs.RabbitMQ.LogQueue) // from InitDefaults
}

func TestDestinationConfig(t *testing.T) {
	tests := []struct {
		name    string
		files   map[string][]byte
		envVars map[string]string
		want    string // expected header prefix
	}{
		{
			name:    "default header prefix",
			files:   map[string][]byte{},
			envVars: map[string]string{},
			want:    "x-outpost-",
		},
		{
			name: "yaml config header prefix",
			files: map[string][]byte{
				"config.yaml": []byte(`
destinations:
  webhook:
    header_prefix: "x-custom-"
`),
			},
			envVars: map[string]string{
				"CONFIG": "config.yaml",
			},
			want: "x-custom-",
		},
		{
			name: "env overrides yaml config",
			files: map[string][]byte{
				"config.yaml": []byte(`
destinations:
  webhook:
    header_prefix: "x-custom-"
`),
			},
			envVars: map[string]string{
				"CONFIG":                             "config.yaml",
				"DESTINATIONS_WEBHOOK_HEADER_PREFIX": "x-env-",
			},
			want: "x-env-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockOS := &mockOS{
				files:   tt.files,
				envVars: tt.envVars,
			}

			cfg, err := config.ParseWithoutValidation(config.Flags{}, mockOS)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, cfg.Destinations.Webhook.HeaderPrefix)
		})
	}
}
