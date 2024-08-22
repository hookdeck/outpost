package config

import (
	"log"
	"strconv"

	"github.com/spf13/viper"
)

var (
	Port int

	RedisHost     string
	RedisPort     int
	RedisPassword string
	RedisDatabase int
)

var defaultConfig = map[string]any{
	"PORT":           3333,
	"REDIS_HOST":     "127.0.0.1",
	"REDIS_PORT":     6379,
	"REDIS_PASSWORD": "",
	"REDIS_DATABASE": 0,
}

func Parse(configFile string) error {
	for key, value := range defaultConfig {
		viper.SetDefault(key, value)
	}

	if configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return err
		}
	}

	viper.AutomaticEnv()

	Port = mustInt("PORT")
	RedisHost = viper.GetString("REDIS_HOST")
	RedisPort = mustInt("REDIS_PORT")
	RedisPassword = viper.GetString("REDIS_PASSWORD")
	RedisDatabase = mustInt("REDIS_DATABASE")

	return nil
}

func mustInt(configName string) int {
	i, err := strconv.Atoi(viper.GetString(configName))
	if err != nil {
		log.Fatalf("%s = '%s' is not int", configName, viper.GetString(configName))
	}
	return i
}
