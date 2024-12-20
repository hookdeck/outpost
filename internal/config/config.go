package config

import (
	"errors"
	"log"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/hookdeck/outpost/internal/clickhouse"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/otel"
	"github.com/hookdeck/outpost/internal/redis"
	"github.com/joho/godotenv"
	v "github.com/spf13/viper"
)

const (
	Namespace = "Outpost"
)

type Config struct {
	Service  ServiceType
	Hostname string

	OpenTelemetry *otel.OpenTelemetryConfig

	// API
	Port         int
	APIKey       string
	APIJWTSecret string

	// Application
	AESEncryptionSecret string
	Topics              []string

	// Infrastructure
	Redis      *redis.RedisConfig
	ClickHouse *clickhouse.ClickHouseConfig
	// MQs
	PublishQueueConfig  *mqs.QueueConfig
	DeliveryQueueConfig *mqs.QueueConfig
	LogQueueConfig      *mqs.QueueConfig

	// Consumers
	PublishMaxConcurrency  int
	DeliveryMaxConcurrency int
	LogMaxConcurrency      int

	// Delivery Retry
	RetryIntervalSeconds int
	RetryMaxLimit        int

	// Event Delivery
	MaxDestinationsPerTenant int
	DeliveryTimeoutSeconds   int

	// Destination Registry
	DestinationMetadataPath string

	// Log batcher configuration
	LogBatcherDelayThresholdSeconds int
	LogBatcherItemCountThreshold    int

	DisableTelemetry bool

	// Destwebhook
	DestinationWebhookHeaderPrefix                  string
	DestinationWebhookDisableDefaultEventIDHeader   bool
	DestinationWebhookDisableDefaultSignatureHeader bool
	DestinationWebhookDisableDefaultTimestampHeader bool
	DestinationWebhookDisableDefaultTopicHeader     bool
	DestinationWebhookSignatureContentTemplate      string
	DestinationWebhookSignatureHeaderTemplate       string
	DestinationWebhookSignatureEncoding             string
	DestinationWebhookSignatureAlgorithm            string

	// Portal config
	PortalRefererURL             string
	PortalFaviconURL             string
	PortalLogo                   string
	PortalOrgName                string
	PortalForceTheme             string
	PortalDisableOutpostBranding bool

	// Dev
	PortalProxyURL string
}

var defaultConfig = map[string]any{
	// Infrastructure
	"PORT":           3333,
	"REDIS_HOST":     "127.0.0.1",
	"REDIS_PORT":     6379,
	"REDIS_PASSWORD": "",
	"REDIS_DATABASE": 0,
	// MQs
	"DELIVERY_RABBITMQ_EXCHANGE": "outpost",
	"DELIVERY_RABBITMQ_QUEUE":    "outpost.delivery",
	"LOG_RABBITMQ_EXCHANGE":      "outpost_logs",
	"LOG_RABBITMQ_QUEUE":         "outpost_logs.log",
	// MQ Publishers
	"DELIVERY_RETRY_LIMIT": 5,
	"LOG_RETRY_LIMIT":      5,
	// Consumers
	"PUBLISH_MAX_CONCURRENCY":  1,
	"DELIVERY_MAX_CONCURRENCY": 1,
	"LOG_MAX_CONCURRENCY":      1,
	// Delivery Retry
	"RETRY_INTERVAL_SECONDS": 30,
	"MAX_RETRY_LIMIT":        10,
	// Event Delivery
	"DELIVERY_TIMEOUT_SECONDS": 5,
	// Log batcher configuration
	"LOG_BATCH_THRESHOLD_SECONDS": 10,
	"LOG_BATCH_SIZE":              1000,
	// Misc
	"MAX_DESTINATIONS_PER_TENANT": 20,
	"DESTINATION_METADATA_PATH":   "config/outpost/destinations",
	// Destination webhook config
	"DESTINATION_WEBHOOK_HEADER_PREFIX": "x-outpost-",
}

var (
	ErrMismatchedServiceType = errors.New("service type mismatch")
)

func Parse(flags Flags) (*Config, error) {
	var err error

	// Use a local Viper instance to avoid leaking configuration to global scope
	viper := v.New()

	hostname, err := os.Hostname()
	if err != nil {
		return nil, err
	}

	// Load .env file to environment variables
	err = godotenv.Load()
	if err != nil {
		// Ignore error if file does not exist
	}

	// Set default config values
	for key, value := range defaultConfig {
		viper.SetDefault(key, value)
	}

	// Parse custom config file if provided
	if flags.Config != "" {
		viper.SetConfigFile(flags.Config)
		if err := viper.ReadInConfig(); err != nil {
			return nil, err
		}
	}

	// Bind environemnt variable to viper
	viper.AutomaticEnv()

	// Parse service type from flag & env
	service, err := parseService(viper, flags)
	if err != nil {
		return nil, err
	}

	openTelemetry, err := parseOpenTelemetryConfig(viper)
	if err != nil {
		return nil, err
	}

	var clickHouseConfig *clickhouse.ClickHouseConfig
	if viper.GetString("CLICKHOUSE_ADDR") != "" {
		clickHouseConfig = &clickhouse.ClickHouseConfig{
			Addr:     viper.GetString("CLICKHOUSE_ADDR"),
			Username: viper.GetString("CLICKHOUSE_USERNAME"),
			Password: viper.GetString("CLICKHOUSE_PASSWORD"),
			Database: viper.GetString("CLICKHOUSE_DATABASE"),
		}
	}

	// MQs
	publishQueueConfig, err := mqs.ParseQueueConfig(viper, "PUBLISH")
	if err != nil {
		return nil, err
	}
	deliveryQueueConfig, err := mqs.ParseQueueConfig(viper, "DELIVERY")
	if err != nil {
		return nil, err
	}
	deliveryQueueConfig.Policy.RetryLimit = viper.GetInt("DELIVERY_RETRY_LIMIT")
	logQueueConfig, err := mqs.ParseQueueConfig(viper, "LOG")
	if err != nil {
		return nil, err
	}
	logQueueConfig.Policy.RetryLimit = viper.GetInt("LOG_RETRY_LIMIT")

	portalProxyURL := viper.GetString("PORTAL_PROXY_URL")
	if portalProxyURL != "" {
		if _, err := url.Parse(portalProxyURL); err != nil {
			return nil, err
		}
	}

	// Initialize config values
	config := &Config{
		Hostname:                 hostname,
		Service:                  *service,
		Port:                     getPort(viper),
		APIKey:                   viper.GetString("API_KEY"),
		APIJWTSecret:             viper.GetString("API_JWT_SECRET"),
		AESEncryptionSecret:      viper.GetString("AES_ENCRYPTION_SECRET"),
		PortalProxyURL:           portalProxyURL,
		Topics:                   parseTopics(viper),
		MaxDestinationsPerTenant: mustInt(viper, "MAX_DESTINATIONS_PER_TENANT"),
		Redis: &redis.RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     mustInt(viper, "REDIS_PORT"),
			Password: viper.GetString("REDIS_PASSWORD"),
			Database: mustInt(viper, "REDIS_DATABASE"),
		},
		ClickHouse:                      clickHouseConfig,
		OpenTelemetry:                   openTelemetry,
		PublishQueueConfig:              publishQueueConfig,
		DeliveryQueueConfig:             deliveryQueueConfig,
		LogQueueConfig:                  logQueueConfig,
		PublishMaxConcurrency:           mustInt(viper, "PUBLISH_MAX_CONCURRENCY"),
		DeliveryMaxConcurrency:          mustInt(viper, "DELIVERY_MAX_CONCURRENCY"),
		LogMaxConcurrency:               mustInt(viper, "LOG_MAX_CONCURRENCY"),
		RetryIntervalSeconds:            mustInt(viper, "RETRY_INTERVAL_SECONDS"),
		RetryMaxLimit:                   mustInt(viper, "MAX_RETRY_LIMIT"),
		DeliveryTimeoutSeconds:          mustInt(viper, "DELIVERY_TIMEOUT_SECONDS"),
		LogBatcherDelayThresholdSeconds: mustInt(viper, "LOG_BATCH_THRESHOLD_SECONDS"),
		LogBatcherItemCountThreshold:    mustInt(viper, "LOG_BATCH_SIZE"),
		DestinationMetadataPath:         viper.GetString("DESTINATION_METADATA_PATH"),

		DisableTelemetry: viper.GetBool("DISABLE_TELEMETRY"),

		// Destination webhook config
		DestinationWebhookHeaderPrefix:                  viper.GetString("DESTINATION_WEBHOOK_HEADER_PREFIX"),
		DestinationWebhookDisableDefaultEventIDHeader:   viper.GetBool("DESTINATION_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER"),
		DestinationWebhookDisableDefaultSignatureHeader: viper.GetBool("DESTINATION_WEBHOOK_DISABLE_DEFAULT_SIGNATURE_HEADER"),
		DestinationWebhookDisableDefaultTimestampHeader: viper.GetBool("DESTINATION_WEBHOOK_DISABLE_DEFAULT_TIMESTAMP_HEADER"),
		DestinationWebhookDisableDefaultTopicHeader:     viper.GetBool("DESTINATION_WEBHOOK_DISABLE_DEFAULT_TOPIC_HEADER"),
		DestinationWebhookSignatureContentTemplate:      viper.GetString("DESTINATION_WEBHOOK_SIGNATURE_CONTENT_TEMPLATE"),
		DestinationWebhookSignatureHeaderTemplate:       viper.GetString("DESTINATION_WEBHOOK_SIGNATURE_HEADER_TEMPLATE"),
		DestinationWebhookSignatureEncoding:             viper.GetString("DESTINATION_WEBHOOK_SIGNATURE_ENCODING"),
		DestinationWebhookSignatureAlgorithm:            viper.GetString("DESTINATION_WEBHOOK_SIGNATURE_ALGORITHM"),

		// Portal config
		PortalRefererURL:             viper.GetString("PORTAL_REFERER_URL"),
		PortalFaviconURL:             viper.GetString("PORTAL_FAVICON_URL"),
		PortalLogo:                   viper.GetString("PORTAL_LOGO"),
		PortalOrgName:                viper.GetString("PORTAL_ORGANIZATION_NAME"),
		PortalForceTheme:             viper.GetString("PORTAL_FORCE_THEME"),
		PortalDisableOutpostBranding: viper.GetBool("PORTAL_DISABLE_OUTPOST_BRANDING"),
	}

	return config, nil
}

func mustInt(viper *v.Viper, configName string) int {
	i, err := strconv.Atoi(viper.GetString(configName))
	if err != nil {
		log.Fatalf("%s = '%s' is not int", configName, viper.GetString(configName))
	}
	return i
}

func getPort(viper *v.Viper) int {
	port := mustInt(viper, "PORT")
	if viper.GetString("API_PORT") != "" {
		apiPort, err := strconv.Atoi(viper.GetString("API_PORT"))
		if err == nil {
			port = apiPort
		}
	}
	return port
}

func parseService(viper *v.Viper, flags Flags) (*ServiceType, error) {
	serviceString := flags.Service
	if viper.GetString("SERVICE") != "" {
		if serviceString != "" && serviceString != viper.GetString("SERVICE") {
			return nil, ErrMismatchedServiceType
		}
		serviceString = viper.GetString("SERVICE")
	}
	service, err := ServiceTypeFromString(serviceString)
	if err != nil {
		return nil, err
	}
	return &service, nil
}

func parseTopics(viper *v.Viper) []string {
	topicStr := viper.GetString("TOPICS")
	if topicStr == "" {
		return []string{}
	}
	topics := strings.Split(topicStr, ",")
	for i, topic := range topics {
		topics[i] = strings.TrimSpace(topic)
	}
	sort.Strings(topics)
	return topics
}
