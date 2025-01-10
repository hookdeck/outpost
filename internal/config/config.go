package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/caarlos0/env/v9"
	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const (
	Namespace = "Outpost"
)

func getConfigLocations() []string {
	return []string{
		// Relative paths
		".env",
		".outpost.yaml",
		"config/outpost.yaml",
		"config/outpost/config.yaml",
		"config/outpost/.env",

		// Container-friendly absolute paths
		"/config/outpost.yaml",
		"/config/outpost/config.yaml",
		"/config/outpost/.env",
	}
}

type Config struct {
	Service       ServiceType          `yaml:"service" env:"SERVICE"`
	OpenTelemetry *OpenTelemetryConfig `yaml:"open_telemetry"`

	// API
	Port         int    `yaml:"port" env:"PORT"`
	APIKey       string `yaml:"api_key" env:"API_KEY"`
	APIJWTSecret string `yaml:"api_jwt_secret" env:"API_JWT_SECRET"`

	// Application
	AESEncryptionSecret string   `yaml:"aes_encryption_secret" env:"AES_ENCRYPTION_SECRET"`
	Topics              []string `yaml:"topics" env:"TOPICS" envSeparator:","`

	// Infrastructure
	Redis      *RedisConfig      `yaml:"redis"`
	ClickHouse *ClickHouseConfig `yaml:"clickhouse"`
	MQs        *MQsConfig        `yaml:"mqs"`

	// PublishMQ
	PublishMQ *PublishMQConfig `yaml:"publishmq"`

	// Consumers
	PublishMaxConcurrency  int `yaml:"publish_max_concurrency" env:"PUBLISH_MAX_CONCURRENCY"`
	DeliveryMaxConcurrency int `yaml:"delivery_max_concurrency" env:"DELIVERY_MAX_CONCURRENCY"`
	LogMaxConcurrency      int `yaml:"log_max_concurrency" env:"LOG_MAX_CONCURRENCY"`

	// Delivery Retry
	RetryIntervalSeconds int `yaml:"retry_interval_seconds" env:"RETRY_INTERVAL_SECONDS"`
	RetryMaxLimit        int `yaml:"retry_max_limit" env:"MAX_RETRY_LIMIT"`

	// Event Delivery
	MaxDestinationsPerTenant int `yaml:"max_destinations_per_tenant" env:"MAX_DESTINATIONS_PER_TENANT"`
	DeliveryTimeoutSeconds   int `yaml:"delivery_timeout_seconds" env:"DELIVERY_TIMEOUT_SECONDS"`

	// Destination Registry
	DestinationMetadataPath string `yaml:"destination_metadata_path" env:"DESTINATION_METADATA_PATH"`

	// Log batcher configuration
	LogBatcherDelayThresholdSeconds int `yaml:"log_batcher_delay_threshold_seconds" env:"LOG_BATCH_THRESHOLD_SECONDS"`
	LogBatcherItemCountThreshold    int `yaml:"log_batcher_item_count_threshold" env:"LOG_BATCH_SIZE"`

	DisableTelemetry bool `yaml:"disable_telemetry" env:"DISABLE_TELEMETRY"`

	// Destwebhook
	DestinationWebhookHeaderPrefix                  string `yaml:"destination_webhook_header_prefix" env:"DESTINATION_WEBHOOK_HEADER_PREFIX"`
	DestinationWebhookDisableDefaultEventIDHeader   bool   `yaml:"destination_webhook_disable_default_event_id_header" env:"DESTINATION_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER"`
	DestinationWebhookDisableDefaultSignatureHeader bool   `yaml:"destination_webhook_disable_default_signature_header" env:"DESTINATION_WEBHOOK_DISABLE_DEFAULT_SIGNATURE_HEADER"`
	DestinationWebhookDisableDefaultTimestampHeader bool   `yaml:"destination_webhook_disable_default_timestamp_header" env:"DESTINATION_WEBHOOK_DISABLE_DEFAULT_TIMESTAMP_HEADER"`
	DestinationWebhookDisableDefaultTopicHeader     bool   `yaml:"destination_webhook_disable_default_topic_header" env:"DESTINATION_WEBHOOK_DISABLE_DEFAULT_TOPIC_HEADER"`
	DestinationWebhookSignatureContentTemplate      string `yaml:"destination_webhook_signature_content_template" env:"DESTINATION_WEBHOOK_SIGNATURE_CONTENT_TEMPLATE"`
	DestinationWebhookSignatureHeaderTemplate       string `yaml:"destination_webhook_signature_header_template" env:"DESTINATION_WEBHOOK_SIGNATURE_HEADER_TEMPLATE"`
	DestinationWebhookSignatureEncoding             string `yaml:"destination_webhook_signature_encoding" env:"DESTINATION_WEBHOOK_SIGNATURE_ENCODING"`
	DestinationWebhookSignatureAlgorithm            string `yaml:"destination_webhook_signature_algorithm" env:"DESTINATION_WEBHOOK_SIGNATURE_ALGORITHM"`

	// Portal config
	PortalRefererURL             string `yaml:"portal_referer_url" env:"PORTAL_REFERER_URL"`
	PortalFaviconURL             string `yaml:"portal_favicon_url" env:"PORTAL_FAVICON_URL"`
	PortalLogo                   string `yaml:"portal_logo" env:"PORTAL_LOGO"`
	PortalOrgName                string `yaml:"portal_org_name" env:"PORTAL_ORGANIZATION_NAME"`
	PortalForceTheme             string `yaml:"portal_force_theme" env:"PORTAL_FORCE_THEME"`
	PortalDisableOutpostBranding bool   `yaml:"portal_disable_outpost_branding" env:"PORTAL_DISABLE_OUTPOST_BRANDING"`

	// Dev
	PortalProxyURL string `yaml:"portal_proxy_url" env:"PORTAL_PROXY_URL"`
}

var (
	ErrMismatchedServiceType = errors.New("service type mismatch")
)

func (c *Config) initDefaults() {
	c.Port = 3333
	c.Redis = &RedisConfig{
		Host: "127.0.0.1",
		Port: 6379,
	}
	c.MQs = &MQsConfig{
		RabbitMQ: &RabbitMQConfig{
			Exchange:      "outpost",
			DeliveryQueue: "outpost-delivery",
			LogQueue:      "outpost-log",
		},
		DeliveryRetryLimit: 5,
		LogRetryLimit:      5,
	}
	c.PublishMaxConcurrency = 1
	c.DeliveryMaxConcurrency = 1
	c.LogMaxConcurrency = 1
	c.RetryIntervalSeconds = 30
	c.RetryMaxLimit = 10
	c.MaxDestinationsPerTenant = 20
	c.DeliveryTimeoutSeconds = 5
	c.DestinationMetadataPath = "config/outpost/destinations"
	c.LogBatcherDelayThresholdSeconds = 10
	c.LogBatcherItemCountThreshold = 1000
	c.DestinationWebhookHeaderPrefix = "x-outpost-"
}

func (c *Config) parseConfigFile(flagPath string, osInterface OSInterface) error {
	// Get config file path from flag or env
	configPath := flagPath
	if envPath := osInterface.Getenv("CONFIG"); envPath != "" {
		if configPath != "" && configPath != envPath {
			return fmt.Errorf("conflicting config paths: flag=%s env=%s", configPath, envPath)
		}
		configPath = envPath
	}

	// If no explicit config path, try default locations
	if configPath == "" {
		for _, loc := range getConfigLocations() {
			if _, err := osInterface.Stat(loc); err == nil {
				configPath = loc
				break
			}
		}
	}

	if configPath == "" {
		return nil
	}

	data, err := osInterface.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("error reading config file: %w", err)
	}

	// Parse based on file extension
	if strings.HasSuffix(strings.ToLower(configPath), ".env") {
		envMap, err := godotenv.Read(configPath)
		if err != nil {
			return fmt.Errorf("error loading .env file: %w", err)
		}
		if err := env.ParseWithOptions(c, env.Options{
			Environment: envMap,
		}); err != nil {
			return fmt.Errorf("error parsing .env file: %w", err)
		}
	} else {
		if err := yaml.Unmarshal(data, c); err != nil {
			return fmt.Errorf("error parsing yaml config: %w", err)
		}
	}
	return nil
}

func (c *Config) parseEnvVariables() error {
	if err := env.Parse(c); err != nil {
		return fmt.Errorf("error parsing environment variables: %w", err)
	}
	return nil
}

func (c *Config) validate(flags Flags) error {
	// Parse service type from flag & env
	service, err := ServiceTypeFromString(flags.Service)
	if err != nil {
		return err
	}
	var zeroService ServiceType
	if c.Service == zeroService {
		c.Service = service
	} else if c.Service != service {
		return ErrMismatchedServiceType
	}

	if c.PortalProxyURL != "" {
		if _, err := url.Parse(c.PortalProxyURL); err != nil {
			return err
		}
	}
	return nil
}

func Parse(flags Flags) (*Config, error) {
	return ParseWithOS(flags, defaultOS)
}

func ParseWithOS(flags Flags, osInterface OSInterface) (*Config, error) {
	var config Config

	// Initialize defaults
	config.initDefaults()

	// Parse config file
	if err := config.parseConfigFile(flags.Config, osInterface); err != nil {
		return nil, err
	}

	// Parse environment variables (highest priority)
	if err := config.parseEnvVariables(); err != nil {
		return nil, err
	}

	// Validate configuration
	if err := config.validate(flags); err != nil {
		return nil, err
	}

	return &config, nil
}

type RedisConfig struct {
	Host     string `yaml:"host" env:"REDIS_HOST"`
	Port     int    `yaml:"port" env:"REDIS_PORT"`
	Password string `yaml:"password" env:"REDIS_PASSWORD"`
	Database int    `yaml:"database" env:"REDIS_DATABASE"`
}

func (c *RedisConfig) ToConfig() *redis.RedisConfig {
	return &redis.RedisConfig{
		Host:     c.Host,
		Port:     c.Port,
		Password: c.Password,
		Database: c.Database,
	}
}

type ClickHouseConfig struct {
	Addr     string `yaml:"addr" env:"CLICKHOUSE_ADDR"`
	Username string `yaml:"username" env:"CLICKHOUSE_USERNAME"`
	Password string `yaml:"password" env:"CLICKHOUSE_PASSWORD"`
	Database string `yaml:"database" env:"CLICKHOUSE_DATABASE"`
}

func (c *ClickHouseConfig) ToConfig() *clickhouse.ClickHouseConfig {
	return &clickhouse.ClickHouseConfig{
		Addr:     c.Addr,
		Username: c.Username,
		Password: c.Password,
		Database: c.Database,
	}
}
