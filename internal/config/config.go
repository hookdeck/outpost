package config

import (
	"log"
	"os"
	"strconv"

	"github.com/hookdeck/EventKit/internal/ingest"
	"github.com/hookdeck/EventKit/internal/otel"
	"github.com/hookdeck/EventKit/internal/redis"
	"github.com/joho/godotenv"
	v "github.com/spf13/viper"
)

const (
	Namespace = "EventKit"
)

type Config struct {
	Service   ServiceType
	Port      int
	Hostname  string
	APIKey    string
	JWTSecret string

	Redis         *redis.RedisConfig
	OpenTelemetry *otel.OpenTelemetryConfig
	Ingest        *ingest.IngestConfig
}

var defaultConfig = map[string]any{
	"PORT":           3333,
	"REDIS_HOST":     "127.0.0.1",
	"REDIS_PORT":     6379,
	"REDIS_PASSWORD": "",
	"REDIS_DATABASE": 0,
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

	ingestConfig, err := ingest.ParseIngestConfig(viper)
	if err != nil {
		return nil, err
	}

	// Initialize config values
	config := &Config{
		Hostname:  hostname,
		Service:   service,
		Port:      getPort(viper),
		APIKey:    viper.GetString("API_KEY"),
		JWTSecret: viper.GetString("JWT_SECRET"),
		Redis: &redis.RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     mustInt(viper, "REDIS_PORT"),
			Password: viper.GetString("REDIS_PASSWORD"),
			Database: mustInt(viper, "REDIS_DATABASE"),
		},
		OpenTelemetry: openTelemetry,
		Ingest:        ingestConfig,
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
