package config

import (
	"log"
	"os"
	"strconv"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

const (
	Namespace = "EventKit"
)

var (
	Service  ServiceType
	Port     int
	Hostname string

	RedisHost     string
	RedisPort     int
	RedisPassword string
	RedisDatabase int

	OpenTelemetry *OpenTelemetryConfig
)

var defaultConfig = map[string]any{
	"PORT":           3333,
	"REDIS_HOST":     "127.0.0.1",
	"REDIS_PORT":     6379,
	"REDIS_PASSWORD": "",
	"REDIS_DATABASE": 0,
}

func Parse(flags Flags) error {
	var err error

	Hostname, err = os.Hostname()
	if err != nil {
		return err
	}

	// Load .env file to environment variables
	err = godotenv.Load()
	if err != nil {
		// Ignore error if file does not exist
	}

	// Parse service type from flag
	Service, err = ServiceTypeFromString(flags.Service)
	if err != nil {
		return err
	}

	// Set default config values
	for key, value := range defaultConfig {
		viper.SetDefault(key, value)
	}

	// Parse custom config file if provided
	if flags.Config != "" {
		viper.SetConfigFile(flags.Config)
		if err := viper.ReadInConfig(); err != nil {
			return err
		}
	}

	// Bind environemnt variable to viper
	viper.AutomaticEnv()

	// Initialize config values
	Port = mustInt("PORT")
	RedisHost = viper.GetString("REDIS_HOST")
	RedisPort = mustInt("REDIS_PORT")
	RedisPassword = viper.GetString("REDIS_PASSWORD")
	RedisDatabase = mustInt("REDIS_DATABASE")

	OpenTelemetry, err = parseOpenTelemetryConfig()
	if err != nil {
		return err
	}

	return nil
}

func mustInt(configName string) int {
	i, err := strconv.Atoi(viper.GetString(configName))
	if err != nil {
		log.Fatalf("%s = '%s' is not int", configName, viper.GetString(configName))
	}
	return i
}
