package config

import (
	"log"
	"os"
	"strconv"

	"github.com/hookdeck/EventKit/internal/clickhouse"
	"github.com/hookdeck/EventKit/internal/mqs"
	"github.com/hookdeck/EventKit/internal/otel"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/joho/godotenv"
	v "github.com/spf13/viper"
)

const (
	Namespace = "EventKit"
)

type Config struct {
	Service          ServiceType
	Port             int
	Hostname         string
	APIKey           string
	JWTSecret        string
	EncryptionSecret string

	Redis                  *redis.RedisConfig
	ClickHouse             *clickhouse.ClickHouseConfig
	OpenTelemetry          *otel.OpenTelemetryConfig
	PublishQueueConfig     *mqs.QueueConfig
	DeliveryQueueConfig    *mqs.QueueConfig
	LogQueueConfig         *mqs.QueueConfig
	PublishMaxConcurrency  int
	DeliveryMaxConcurrency int
	LogMaxConcurrency      int
	RetryIntervalSeconds   int
}

var defaultConfig = map[string]any{
	"PORT":                       3333,
	"REDIS_HOST":                 "127.0.0.1",
	"REDIS_PORT":                 6379,
	"REDIS_PASSWORD":             "",
	"REDIS_DATABASE":             0,
	"DELIVERY_RABBITMQ_EXCHANGE": "eventkit",
	"DELIVERY_RABBITMQ_QUEUE":    "eventkit.delivery",
	"LOG_RABBITMQ_EXCHANGE":      "eventkit_logs",
	"LOG_RABBITMQ_QUEUE":         "eventkit_logs.log",
	"PUBLISHMQ_MAX_CONCURRENCY":  1,
	"DELIVERYMQ_MAX_CONCURRENCY": 1,
	"LOGMQ_MAX_CONCURRENCY":      1,
	"RETRY_INTERVAL_SECONDS":     30,
}

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

	// Parse service type from flag
	service, err := ServiceTypeFromString(flags.Service)
	if err != nil {
		return nil, err
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
	logQueueConfig, err := mqs.ParseQueueConfig(viper, "LOG")
	if err != nil {
		return nil, err
	}

	// Initialize config values
	config := &Config{
		Hostname:         hostname,
		Service:          service,
		Port:             getPort(viper),
		APIKey:           viper.GetString("API_KEY"),
		JWTSecret:        viper.GetString("JWT_SECRET"),
		EncryptionSecret: viper.GetString("ENCRYPTION_SECRET"),
		Redis: &redis.RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     mustInt(viper, "REDIS_PORT"),
			Password: viper.GetString("REDIS_PASSWORD"),
			Database: mustInt(viper, "REDIS_DATABASE"),
		},
		ClickHouse:             clickHouseConfig,
		OpenTelemetry:          openTelemetry,
		PublishQueueConfig:     publishQueueConfig,
		DeliveryQueueConfig:    deliveryQueueConfig,
		LogQueueConfig:         logQueueConfig,
		PublishMaxConcurrency:  mustInt(viper, "PUBLISHMQ_MAX_CONCURRENCY"),
		DeliveryMaxConcurrency: mustInt(viper, "DELIVERYMQ_MAX_CONCURRENCY"),
		LogMaxConcurrency:      mustInt(viper, "LOGMQ_MAX_CONCURRENCY"),
		RetryIntervalSeconds:   mustInt(viper, "RETRY_INTERVAL_SECONDS"),
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
