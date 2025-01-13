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

	if err := c.validateClickHouse(); err != nil {
		return err
	}

	if err := c.validateMQs(); err != nil {
		return err
	}

	if err := c.validateAESEncryptionSecret(); err != nil {
		return err
	}

	if err := c.validatePortalProxyURL(); err != nil {
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

	// If service is set in config (via env or file), it must match flag
	if c.Service != "" && configService != flagService {
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
	if c.Redis == nil || c.Redis.Host == "" {
		return ErrMissingRedis
	}
	return nil
}

// validateClickHouse validates the ClickHouse configuration
func (c *Config) validateClickHouse() error {
	if c.ClickHouse == nil || c.ClickHouse.Addr == "" {
		return ErrMissingClickHouse
	}
	return nil
}

// validateMQs validates the MQs configuration
func (c *Config) validateMQs() error {
	if c.MQs == nil || c.MQs.RabbitMQ == nil {
		return ErrMissingMQs
	}

	// Check delivery queue
	deliveryConfig := c.MQs.GetDeliveryQueueConfig()
	if deliveryConfig == nil {
		return ErrMissingMQs
	}

	// Check log queue
	logConfig := c.MQs.GetLogQueueConfig()
	if logConfig == nil {
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
func (c *Config) validatePortalProxyURL() error {
	if c.PortalProxyURL != "" {
		if _, err := url.Parse(c.PortalProxyURL); err != nil {
			return ErrInvalidPortalProxyURL
		}
	}
	return nil
}
