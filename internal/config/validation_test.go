package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
)

// validConfig returns a config with all required fields set
func validConfig() *config.Config {
	return &config.Config{
		Redis: &config.RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		ClickHouse: &config.ClickHouseConfig{
			Addr: "localhost:9000",
		},
		MQs: &config.MQsConfig{
			RabbitMQ: &config.RabbitMQConfig{
				ServerURL:     "amqp://localhost:5672",
				Exchange:      "outpost",
				DeliveryQueue: "outpost-delivery",
				LogQueue:      "outpost-log",
			},
		},
		AESEncryptionSecret: "secret",
	}
}

func TestValidateService(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		flags   config.Flags
		wantErr error
	}{
		{
			name: "empty service takes flag value",
			config: func() *config.Config {
				c := validConfig()
				c.Service = ""
				return c
			}(),
			flags: config.Flags{
				Service: "api",
			},
			wantErr: nil,
		},
		{
			name: "matching service types",
			config: func() *config.Config {
				c := validConfig()
				c.Service = "api"
				return c
			}(),
			flags: config.Flags{
				Service: "api",
			},
			wantErr: nil,
		},
		{
			name: "mismatched service types",
			config: func() *config.Config {
				c := validConfig()
				c.Service = "api"
				return c
			}(),
			flags: config.Flags{
				Service: "delivery",
			},
			wantErr: config.ErrMismatchedServiceType,
		},
		{
			name: "invalid service type in flag",
			config: func() *config.Config {
				c := validConfig()
				c.Service = ""
				return c
			}(),
			flags: config.Flags{
				Service: "invalid",
			},
			wantErr: config.ErrInvalidServiceType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(tt.flags)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				// If no error, check that service was set correctly
				if tt.config.Service == "" {
					assert.Equal(t, tt.flags.Service, tt.config.Service)
				}
			}
		})
	}
}

func TestRedis(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantErr error
	}{
		{
			name:    "valid redis config",
			config:  validConfig(),
			wantErr: nil,
		},
		{
			name: "missing redis config",
			config: func() *config.Config {
				c := validConfig()
				c.Redis = nil
				return c
			}(),
			wantErr: config.ErrMissingRedis,
		},
		{
			name: "missing redis host",
			config: func() *config.Config {
				c := validConfig()
				c.Redis.Host = ""
				return c
			}(),
			wantErr: config.ErrMissingRedis,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(config.Flags{})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestClickHouse(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantErr error
	}{
		{
			name:    "valid clickhouse config",
			config:  validConfig(),
			wantErr: nil,
		},
		{
			name: "missing clickhouse config",
			config: func() *config.Config {
				c := validConfig()
				c.ClickHouse = nil
				return c
			}(),
			wantErr: config.ErrMissingClickHouse,
		},
		{
			name: "missing clickhouse addr",
			config: func() *config.Config {
				c := validConfig()
				c.ClickHouse.Addr = ""
				return c
			}(),
			wantErr: config.ErrMissingClickHouse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(config.Flags{})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMQs(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantErr error
	}{
		{
			name:    "valid mqs config",
			config:  validConfig(),
			wantErr: nil,
		},
		{
			name: "missing mqs config",
			config: func() *config.Config {
				c := validConfig()
				c.MQs = nil
				return c
			}(),
			wantErr: config.ErrMissingMQs,
		},
		{
			name: "missing rabbitmq config",
			config: func() *config.Config {
				c := validConfig()
				c.MQs.RabbitMQ = nil
				return c
			}(),
			wantErr: config.ErrMissingMQs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(config.Flags{})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestMisc(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantErr error
	}{
		{
			name:    "valid aes secret",
			config:  validConfig(),
			wantErr: nil,
		},
		{
			name: "missing aes secret",
			config: func() *config.Config {
				c := validConfig()
				c.AESEncryptionSecret = ""
				return c
			}(),
			wantErr: config.ErrMissingAESSecret,
		},
		{
			name:    "valid portal proxy url",
			config:  validConfig(),
			wantErr: nil,
		},
		{
			name: "empty portal proxy url is valid",
			config: func() *config.Config {
				c := validConfig()
				c.PortalProxyURL = ""
				return c
			}(),
			wantErr: nil,
		},
		{
			name: "invalid portal proxy url",
			config: func() *config.Config {
				c := validConfig()
				c.PortalProxyURL = "://invalid"
				return c
			}(),
			wantErr: config.ErrInvalidPortalProxyURL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(config.Flags{})
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
