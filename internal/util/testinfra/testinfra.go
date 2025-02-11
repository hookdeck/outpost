package testinfra

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
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
	PostgresURL   string
	LocalStackURL string
	RabbitMQURL   string
	MockServerURL string
	cleanupFns    []func()
}

func initConfig() {
	projectRoot, err := findProjectRoot()
	if err != nil {
		panic(err)
	}

	v := viper.New()
	v.AutomaticEnv()
	v.SetConfigFile(filepath.Join(projectRoot, ".env.test"))
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		panic(err)
	}

	if v.GetBool("TESTINFRA") {
		localstackURL := v.GetString("TEST_LOCALSTACK_URL")
		if !strings.Contains(localstackURL, "http://") {
			localstackURL = "http://" + localstackURL
		}
		rabbitmqURL := v.GetString("TEST_RABBITMQ_URL")
		if !strings.Contains(rabbitmqURL, "amqp://") {
			rabbitmqURL = "amqp://guest:guest@" + rabbitmqURL
		}
		mockServerURL := v.GetString("TEST_MOCKSERVER_URL")
		if !strings.Contains(mockServerURL, "http://") {
			mockServerURL = "http://" + mockServerURL
		}
		cfg = &Config{
			TestInfra:     v.GetBool("TESTINFRA"),
			ClickHouseURL: v.GetString("TEST_CLICKHOUSE_URL"),
			PostgresURL:   v.GetString("TEST_POSTGRES_URL"),
			LocalStackURL: localstackURL,
			RabbitMQURL:   rabbitmqURL,
			MockServerURL: mockServerURL,
		}
		return
	}

	cfg = &Config{
		TestInfra:     v.GetBool("TESTINFRA"),
		ClickHouseURL: "",
		LocalStackURL: "",
		RabbitMQURL:   "",
		MockServerURL: "",
	}
}

func ReadConfig() *Config {
	cfgSync.Do(initConfig)
	return cfg
}

func Start(t *testing.T) func() {
	testutil.CheckIntegrationTest(t)
	suiteCounter += 1
	return func() {
		suiteCounter -= 1
		if suiteCounter == 0 {
			for _, fn := range cfg.cleanupFns {
				fn()
			}
		}
	}
}

func findProjectRoot() (string, error) {
	// Start from the current working directory
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	// Traverse up the directory tree until the project root is found
	for {
		if _, err := os.Stat(filepath.Join(dir, ".env.test")); err == nil {
			return dir, nil
		}
		parentDir := filepath.Dir(dir)
		if parentDir == dir {
			break
		}
		dir = parentDir
	}

	return "", os.ErrNotExist
}
