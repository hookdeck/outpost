package config

import (
	"net/url"
)

// Validate checks if the configuration is valid
func (c *Config) Validate(flags Flags) error {
	// Reset validated state
	c.validated = false

	// Validate each component
	if err := c.validateService(flags); err != nil {
		return err
	}

	if err := c.validateRedis(); err != nil {
		return err
	}

	if err := c.validateLogStorage(); err != nil {
		return err
	}

	if err := c.validateMQs(); err != nil {
		return err
	}

	if err := c.validateAESEncryptionSecret(); err != nil {
		return err
	}

	if err := c.validatePortal(); err != nil {
		return err
	}

	if err := c.OpenTelemetry.Validate(); err != nil {
		return err
	}

	// Mark as validated if we get here
	c.validated = true
	return nil
}

// validateService validates the service configuration
func (c *Config) validateService(flags Flags) error {
	// Parse service type from flag & env
	flagService, err := ServiceTypeFromString(flags.Service)
	if err != nil {
		return err
	}

	configService, err := c.GetService()
	if err != nil {
		return err
	}

	// If service is set in config, it must match flag (unless flag is empty)
	if c.Service != "" && flags.Service != "" && configService != flagService {
		return ErrMismatchedServiceType
	}

	// If no service set in config, use flag value
	if c.Service == "" {
		c.Service = flags.Service
	}

	return nil
}

// validateRedis validates the Redis configuration
func (c *Config) validateRedis() error {
	if c.Redis.Host == "" {
		return ErrMissingRedis
	}
	return nil
}

// validateLogStorage validates the ClickHouse / PG configuration
func (c *Config) validateLogStorage() error {
	if c.ClickHouse.Addr == "" && c.PostgresURL == "" {
		return ErrMissingLogStorage
	}
	return nil
}

// validateMQs validates the MQs configuration
func (c *Config) validateMQs() error {
	// Check delivery queue
	if c.MQs.GetDeliveryQueueConfig() == nil {
		return ErrMissingMQs
	}

	// Check log queue
	if c.MQs.GetLogQueueConfig() == nil {
		return ErrMissingMQs
	}

	return nil
}

// validateAESEncryptionSecret validates the AES encryption secret
func (c *Config) validateAESEncryptionSecret() error {
	if c.AESEncryptionSecret == "" {
		return ErrMissingAESSecret
	}
	return nil
}

// validatePortalProxyURL validates the portal proxy URL if set
func (c *Config) validatePortal() error {
	if c.Portal.ProxyURL != "" {
		if _, err := url.Parse(c.Portal.ProxyURL); err != nil {
			return ErrInvalidPortalProxyURL
		}
	}
	return nil
}
