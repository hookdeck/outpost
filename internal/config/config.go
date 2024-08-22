package config

import (
	"log"
	"strconv"

	"github.com/spf13/viper"
)

var (
	Port int

	RedisHost     string
	RedisPort     string
	RedisPassword string
	RedisDatabase int
)

func Parse(configFile string) error {
	if configFile != "" {
		viper.SetConfigFile(configFile)
		if err := viper.ReadInConfig(); err != nil {
			return err
		}
	}

	viper.BindEnv("PORT")
	Port = mustInt("PORT")

	viper.BindEnv("REDIS_HOST")
	viper.BindEnv("REDIS_PORT")
	viper.BindEnv("REDIS_PASSWORD")
	viper.BindEnv("REDIS_DATABASE")
	RedisHost = viper.GetString("REDIS_HOST")
	RedisPort = viper.GetString("REDIS_PORT")
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
