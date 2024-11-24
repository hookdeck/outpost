package testinfra

import (
	"log"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

var (
	suiteCounter = 0
	cfgSync      sync.Once
	cfg          *Config
)

type Config struct {
	TestInfra     bool
	ClickHouseURL string
	LocalStackURL string
	RabbitMQURL   string
	cleanupFns    []func()
}

func initConfig() {
	v := viper.New()
	v.AutomaticEnv()
	v.SetConfigFile("../../.env.test")
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		panic(err)
	}

	if v.GetBool("TESTINFRA") {
		localstackURL := v.GetString("TEST_LOCALSTACK_URL")
		if !strings.Contains(localstackURL, "http://") {
			localstackURL = "http://" + localstackURL
		}
		cfg = &Config{
			TestInfra:     v.GetBool("TESTINFRA"),
			ClickHouseURL: v.GetString("TEST_CLICKHOUSE_URL"),
			LocalStackURL: localstackURL,
			RabbitMQURL:   v.GetString("TEST_RABBITMQ_URL"),
		}
		return
	}

	cfg = &Config{
		TestInfra:     v.GetBool("TESTINFRA"),
		ClickHouseURL: "",
		LocalStackURL: "",
		RabbitMQURL:   "",
	}
}

func ReadConfig() *Config {
	cfgSync.Do(initConfig)
	return cfg
}

func Start() func() {
	suiteCounter += 1
	return func() {
		suiteCounter -= 1
		if suiteCounter == 0 {
			log.Println("cleaning up", len(cfg.cleanupFns))
			for _, fn := range cfg.cleanupFns {
				fn()
			}
		}
	}
}
