package config

import (
	"errors"
	"net/url"
	"os"

	"github.com/caarlos0/env/v9"
	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

const (
	Namespace = "Outpost"
)

type RedisConfig struct {
	Host     string `yaml:"host" env:"REDIS_HOST"`
	Port     int    `yaml:"port" env:"REDIS_PORT"`
	Password string `yaml:"password" env:"REDIS_PASSWORD"`
	Database int    `yaml:"database" env:"REDIS_DATABASE"`
}

type ClickHouseConfig struct {
	Addr     string `yaml:"addr" env:"CLICKHOUSE_ADDR"`
	Username string `yaml:"username" env:"CLICKHOUSE_USERNAME"`
	Password string `yaml:"password" env:"CLICKHOUSE_PASSWORD"`
	Database string `yaml:"database" env:"CLICKHOUSE_DATABASE"`
}

type Config struct {
	Service  ServiceType `yaml:"service" env:"SERVICE"`
	Hostname string      `yaml:"hostname"`

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

func Parse(flags Flags) (*Config, error) {
	// Initialize with defaults
	config := &Config{
		Port: 3333,
		Redis: &RedisConfig{
			Host: "127.0.0.1",
			Port: 6379,
		},
		MQs: &MQsConfig{
			RabbitMQ: &RabbitMQConfig{
				Exchange:      "outpost",
				DeliveryQueue: "outpost-delivery",
				LogQueue:      "outpost-log",
			},
			DeliveryRetryLimit: 5,
			LogRetryLimit:      5,
		},
		PublishMaxConcurrency:           1,
		DeliveryMaxConcurrency:          1,
		LogMaxConcurrency:               1,
		RetryIntervalSeconds:            30,
		RetryMaxLimit:                   10,
		MaxDestinationsPerTenant:        20,
		DeliveryTimeoutSeconds:          5,
		DestinationMetadataPath:         "config/outpost/destinations",
		LogBatcherDelayThresholdSeconds: 10,
		LogBatcherItemCountThreshold:    1000,
		DestinationWebhookHeaderPrefix:  "x-outpost-",
	}

	// Load .env file to environment variables
	err := godotenv.Load()
	if err != nil {
		// Ignore error if file does not exist
	}

	// Parse YAML config file if provided (overrides defaults)
	if flags.Config != "" {
		data, err := os.ReadFile(flags.Config)
		if err != nil {
			return nil, err
		}
		if err := yaml.Unmarshal(data, config); err != nil {
			return nil, err
		}
	}

	// Parse environment variables (highest priority)
	if err := env.Parse(config); err != nil {
		return nil, err
	}

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}
	config.Hostname = hostname

	// Parse service type from flag & env
	service, err := ServiceTypeFromString(flags.Service)
	if err != nil {
		return nil, err
	}
	var zeroService ServiceType
	if config.Service == zeroService {
		config.Service = service
	} else if config.Service != service {
		return nil, ErrMismatchedServiceType
	}

	if config.PortalProxyURL != "" {
		if _, err := url.Parse(config.PortalProxyURL); err != nil {
			return nil, err
		}
	}

	return config, nil
}
