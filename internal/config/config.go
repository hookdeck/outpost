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
	Service          ServiceType
	Port             int
	Hostname         string
	APIKey           string
	JWTSecret        string
	EncryptionSecret string
	PortalProxyURL   string
	Topics           []string

	Redis                           *redis.RedisConfig
	ClickHouse                      *clickhouse.ClickHouseConfig
	OpenTelemetry                   *otel.OpenTelemetryConfig
	PublishQueueConfig              *mqs.QueueConfig
	DeliveryQueueConfig             *mqs.QueueConfig
	LogQueueConfig                  *mqs.QueueConfig
	PublishMaxConcurrency           int
	DeliveryMaxConcurrency          int
	LogMaxConcurrency               int
	RetryIntervalSeconds            int
	RetryMaxCount                   int
	LogBatcherDelayThresholdSeconds int
	LogBatcherItemCountThreshold    int
}

var defaultConfig = map[string]any{
	"PORT":                                3333,
	"REDIS_HOST":                          "127.0.0.1",
	"REDIS_PORT":                          6379,
	"REDIS_PASSWORD":                      "",
	"REDIS_DATABASE":                      0,
	"DELIVERY_RABBITMQ_EXCHANGE":          "outpost",
	"DELIVERY_RABBITMQ_QUEUE":             "outpost.delivery",
	"LOG_RABBITMQ_EXCHANGE":               "outpost_logs",
	"LOG_RABBITMQ_QUEUE":                  "outpost_logs.log",
	"PUBLISHMQ_MAX_CONCURRENCY":           1,
	"DELIVERYMQ_MAX_CONCURRENCY":          1,
	"LOGMQ_MAX_CONCURRENCY":               1,
	"RETRY_INTERVAL_SECONDS":              30,
	"MAX_RETRY_COUNT":                     10,
	"LOG_BATCHER_DELAY_THRESHOLD_SECONDS": 5,
	"LOG_BATCHER_ITEM_COUNT_THRESHOLD":    100,
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

	topics, err := parseTopics(viper)
	if err != nil {
		return nil, err
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
	logQueueConfig, err := mqs.ParseQueueConfig(viper, "LOG")
	if err != nil {
		return nil, err
	}

	portalProxyURL := viper.GetString("PORTAL_PROXY_URL")
	if portalProxyURL != "" {
		if _, err := url.Parse(portalProxyURL); err != nil {
			return nil, err
		}
	}

	// Initialize config values
	config := &Config{
		Hostname:         hostname,
		Service:          *service,
		Port:             getPort(viper),
		APIKey:           viper.GetString("API_KEY"),
		JWTSecret:        viper.GetString("JWT_SECRET"),
		EncryptionSecret: viper.GetString("ENCRYPTION_SECRET"),
		PortalProxyURL:   portalProxyURL,
		Topics:           topics,
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
		PublishMaxConcurrency:           mustInt(viper, "PUBLISHMQ_MAX_CONCURRENCY"),
		DeliveryMaxConcurrency:          mustInt(viper, "DELIVERYMQ_MAX_CONCURRENCY"),
		LogMaxConcurrency:               mustInt(viper, "LOGMQ_MAX_CONCURRENCY"),
		RetryIntervalSeconds:            mustInt(viper, "RETRY_INTERVAL_SECONDS"),
		RetryMaxCount:                   mustInt(viper, "MAX_RETRY_COUNT"),
		LogBatcherDelayThresholdSeconds: mustInt(viper, "LOG_BATCHER_DELAY_THRESHOLD_SECONDS"),
		LogBatcherItemCountThreshold:    mustInt(viper, "LOG_BATCHER_ITEM_COUNT_THRESHOLD"),
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

func parseTopics(viper *v.Viper) ([]string, error) {
	topicStr := viper.GetString("TOPICS")
	if topicStr == "" {
		return nil, makeEmptyErr("TOPICS")
	}
	topics := strings.Split(topicStr, ",")
	for i, topic := range topics {
		topics[i] = strings.TrimSpace(topic)
	}
	sort.Strings(topics)
	return topics, nil
}

func makeEmptyErr(key string) error {
	return errors.New(key + " is empty")
}
