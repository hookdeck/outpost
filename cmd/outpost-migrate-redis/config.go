package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/urfave/cli/v3"
)

// ConfigLoader handles configuration loading and validation for the migration tool
type ConfigLoader struct{}

// NewConfigLoader creates a new config loader
func NewConfigLoader() *ConfigLoader {
	return &ConfigLoader{}
}

// LoadConfig loads and validates configuration from files/env and applies CLI overrides
func (cl *ConfigLoader) LoadConfig(c *cli.Command) (*config.Config, error) {
	// Load config using the existing system
	flags := config.Flags{
		Config: c.String("config"),
	}

	// Use ParseWithoutValidation to load config files and env vars
	// without validating all required fields (since we only need Redis config)
	cfg, err := config.ParseWithoutValidation(flags, &defaultOSImpl{})
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Apply Redis CLI overrides
	cl.applyRedisOverrides(c, cfg)

	// Validate Redis configuration
	if err := cl.validateRedisConfig(&cfg.Redis); err != nil {
		return nil, fmt.Errorf("invalid Redis configuration: %w", err)
	}

	return cfg, nil
}

// applyRedisOverrides applies CLI flag overrides to Redis configuration
func (cl *ConfigLoader) applyRedisOverrides(c *cli.Command, cfg *config.Config) error {
	if host := c.String("redis-host"); host != "" {
		cfg.Redis.Host = host
	}

	if portStr := c.String("redis-port"); portStr != "" {
		port, err := strconv.Atoi(portStr)
		if err != nil {
			return fmt.Errorf("invalid redis-port: %w", err)
		}
		cfg.Redis.Port = port
	}

	if password := c.String("redis-password"); password != "" {
		cfg.Redis.Password = password
	}

	if c.IsSet("redis-database") {
		cfg.Redis.Database = c.Int("redis-database")
	}

	if c.IsSet("redis-cluster") {
		cfg.Redis.ClusterEnabled = c.Bool("redis-cluster")
	}

	if c.IsSet("redis-tls") {
		cfg.Redis.TLSEnabled = c.Bool("redis-tls")
	}

	return nil
}

// validateRedisConfig validates the Redis configuration
func (cl *ConfigLoader) validateRedisConfig(rc *config.RedisConfig) error {
	// Basic validation for Redis config
	if rc.Host == "" {
		return fmt.Errorf("redis host is required")
	}
	if rc.Port == 0 {
		return fmt.Errorf("redis port is required")
	}

	// Check for cluster-specific configuration
	if rc.ClusterEnabled {
		if rc.Database != 0 {
			return fmt.Errorf("redis cluster mode doesn't support database selection")
		}
	}

	return nil
}

// defaultOSImpl implements OSInterface for config loading
type defaultOSImpl struct{}

func (d *defaultOSImpl) Getenv(key string) string {
	return os.Getenv(key)
}

func (d *defaultOSImpl) Stat(name string) (os.FileInfo, error) {
	return os.Stat(name)
}

func (d *defaultOSImpl) ReadFile(name string) ([]byte, error) {
	return os.ReadFile(name)
}

func (d *defaultOSImpl) Environ() []string {
	return os.Environ()
}
