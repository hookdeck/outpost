package config_test

import (
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestValidateService(t *testing.T) {
	tests := []struct {
		name    string
		config  config.Config
		flags   config.Flags
		wantErr error
	}{
		{
			name: "empty service type becomes flag value",
			config: config.Config{
				Service: "",
			},
			flags: config.Flags{
				Service: "api",
			},
			wantErr: nil,
		},
		{
			name: "matching service types",
			config: config.Config{
				Service: "api",
			},
			flags: config.Flags{
				Service: "api",
			},
			wantErr: nil,
		},
		{
			name: "mismatched service types",
			config: config.Config{
				Service: "log",
			},
			flags: config.Flags{
				Service: "api",
			},
			wantErr: config.ErrMismatchedServiceType,
		},
		{
			name: "invalid service type in flag",
			config: config.Config{
				Service: "",
			},
			flags: config.Flags{
				Service: "invalid",
			},
			wantErr: config.ErrInvalidServiceType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config // Make a copy to avoid modifying test data
			err := cfg.Validate(tt.flags)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, err)
				// Check that empty service is set to flag value
				if tt.config.Service == "" {
					assert.Equal(t, tt.flags.Service, cfg.Service)
				}
			}
		})
	}
}

func TestValidatePortalProxyURL(t *testing.T) {
	tests := []struct {
		name    string
		config  config.Config
		wantErr bool
	}{
		{
			name: "empty url is valid",
			config: config.Config{
				PortalProxyURL: "",
			},
			wantErr: false,
		},
		{
			name: "valid url",
			config: config.Config{
				PortalProxyURL: "http://localhost:3000",
			},
			wantErr: false,
		},
		{
			name: "invalid url",
			config: config.Config{
				PortalProxyURL: "://invalid",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate(config.Flags{})
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
