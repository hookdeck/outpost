package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
)

// validConfig returns a config with all required fields set
func validConfig() *config.Config {
	c := &config.Config{}
	c.InitDefaults()

	// Override only what's needed for validation
	// c.ClickHouse.Addr = "localhost:9000"
	c.PostgresURL = "postgres://postgres:postgres@localhost:5432/postgres"
	c.MQs.RabbitMQ.ServerURL = "amqp://localhost:5672"
	c.AESEncryptionSecret = "secret"

	return c
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
			name: "config service with empty flag service is valid",
			config: func() *config.Config {
				c := validConfig()
				c.Service = "api"
				return c
			}(),
			flags: config.Flags{
				Service: "",
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
				c.Redis = config.RedisConfig{}
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
			name: "missing storage config",
			config: func() *config.Config {
				c := validConfig()
				c.ClickHouse = config.ClickHouseConfig{}
				c.PostgresURL = ""
				return c
			}(),
			wantErr: config.ErrMissingLogStorage,
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
				c.MQs = config.MQsConfig{}
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
				c.Portal.ProxyURL = ""
				return c
			}(),
			wantErr: nil,
		},
		{
			name: "invalid portal proxy url",
			config: func() *config.Config {
				c := validConfig()
				c.Portal.ProxyURL = "://invalid"
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

func TestOpenTelemetry(t *testing.T) {
	tests := []struct {
		name    string
		config  *config.Config
		wantErr error
	}{
		{
			name:    "empty config is valid",
			config:  validConfig(),
			wantErr: nil,
		},
		{
			name: "valid grpc protocol",
			config: func() *config.Config {
				c := validConfig()
				c.OpenTelemetry = config.OpenTelemetryConfig{
					ServiceName: "test",
					Traces: config.OpenTelemetryTypeConfig{
						Protocol: "grpc",
					},
				}
				return c
			}(),
			wantErr: nil,
		},
		{
			name: "valid http protocol",
			config: func() *config.Config {
				c := validConfig()
				c.OpenTelemetry = config.OpenTelemetryConfig{
					ServiceName: "test",
					Traces: config.OpenTelemetryTypeConfig{
						Protocol: "http",
					},
				}
				return c
			}(),
			wantErr: nil,
		},
		{
			name: "invalid protocol",
			config: func() *config.Config {
				c := validConfig()
				c.OpenTelemetry = config.OpenTelemetryConfig{
					ServiceName: "test",
					Traces: config.OpenTelemetryTypeConfig{
						Protocol: "invalid",
					},
				}
				return c
			}(),
			wantErr: config.ErrInvalidOTelProtocol,
		},
		{
			name: "empty service name disables OpenTelemetry",
			config: func() *config.Config {
				c := validConfig()
				c.OpenTelemetry = config.OpenTelemetryConfig{
					ServiceName: "",
					Traces: config.OpenTelemetryTypeConfig{
						Protocol: "invalid", // Even invalid protocol should be ok when disabled
					},
				}
				return c
			}(),
			wantErr: nil,
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
