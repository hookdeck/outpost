package testinfra

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/spf13/viper"
)

var (
	suiteCounter int64
	suiteCleanup sync.Once
	cfgSync      sync.Once
	cfg          *Config
)

type Config struct {
	TestInfra         bool
	TestAzure         bool
	RedisURL          string
	DragonflyURL      string
	ClickHouseURL     string
	PostgresURL       string
	LocalStackURL     string
	RabbitMQURL       string
	MockServerURL     string
	GCPURL            string
	AzureSBConnString string
	cleanupFns        []func()
}

func initConfig() {
	projectRoot, err := findProjectRoot()
	if err != nil {
		panic(err)
	}

	v := viper.New()
	v.AutomaticEnv()

	// Allow override via environment variable
	configFile := os.Getenv("TEST_CONFIG_FILE")
	if configFile == "" {
		configFile = ".env.test"
	}

	v.SetConfigFile(filepath.Join(projectRoot, configFile))
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
			TestInfra:         v.GetBool("TESTINFRA"),
			TestAzure:         v.GetBool("TESTAZURE"),
			RedisURL:          v.GetString("TEST_REDIS_URL"),
			DragonflyURL:      v.GetString("TEST_DRAGONFLY_URL"),
			ClickHouseURL:     v.GetString("TEST_CLICKHOUSE_URL"),
			PostgresURL:       v.GetString("TEST_POSTGRES_URL"),
			LocalStackURL:     localstackURL,
			GCPURL:            v.GetString("TEST_GCP_URL"),
			AzureSBConnString: v.GetString("TEST_AZURE_SB_CONNSTRING"),
			RabbitMQURL:       rabbitmqURL,
			MockServerURL:     mockServerURL,
		}
		return
	}

	cfg = &Config{
		TestInfra:         v.GetBool("TESTINFRA"),
		TestAzure:         v.GetBool("TESTAZURE"),
		RedisURL:          "",
		DragonflyURL:      "",
		ClickHouseURL:     "",
		PostgresURL:       "",
		LocalStackURL:     "",
		GCPURL:            "",
		AzureSBConnString: "",
		RabbitMQURL:       "",
		MockServerURL:     "",
	}
}

func ReadConfig() *Config {
	cfgSync.Do(initConfig)
	return cfg
}

func Start(t *testing.T) func() {
	testutil.CheckIntegrationTest(t)
	atomic.AddInt64(&suiteCounter, 1)
	return func() {
		if atomic.AddInt64(&suiteCounter, -1) == 0 {
			suiteCleanup.Do(func() {
				// Ensure cfg is initialized and not nil before accessing cleanupFns
				if cfg != nil && cfg.cleanupFns != nil {
					for _, fn := range cfg.cleanupFns {
						if fn != nil {
							fn()
						}
					}
				}
			})
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
