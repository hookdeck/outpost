package testinfra

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/hookdeck/outpost/internal/util/testutil"
	"github.com/spf13/viper"
)

var (
	cfgSync sync.Once
	cfg     *Config
)

type Config struct {
	TestInfra         bool
	TestAzure         bool
	ClickHouseURL     string
	PostgresURL       string
	LocalStackURL     string
	RabbitMQURL       string
	KafkaURL          string
	MockServerURL     string
	GCPURL            string
	AzureSBConnString string
	Images            Images
}

// Images holds the container images testcontainers starts when TESTINFRA is
// unset. They are the same images build/test/compose.yml runs, read from the
// same .env.test, so a test sees one environment either way.
type Images struct {
	Postgres   string
	ClickHouse string
	RabbitMQ   string
	Kafka      string
	LocalStack string
	GCP        string
	Redis      string
	Dragonfly  string
}

func readImages(v *viper.Viper) Images {
	return Images{
		Postgres:   requireImage(v, "TEST_IMAGE_POSTGRES"),
		ClickHouse: requireImage(v, "TEST_IMAGE_CLICKHOUSE"),
		RabbitMQ:   requireImage(v, "TEST_IMAGE_RABBITMQ"),
		Kafka:      requireImage(v, "TEST_IMAGE_KAFKA"),
		LocalStack: requireImage(v, "TEST_IMAGE_LOCALSTACK"),
		GCP:        requireImage(v, "TEST_IMAGE_GCP"),
		Redis:      requireImage(v, "TEST_IMAGE_REDIS"),
		Dragonfly:  requireImage(v, "TEST_IMAGE_DRAGONFLY"),
	}
}

func requireImage(v *viper.Viper, key string) string {
	image := v.GetString(key)
	if image == "" {
		panic(fmt.Errorf("%s is not set; add it to .env.test (see the repo copy for the current pins)", key))
	}
	return image
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
			ClickHouseURL:     v.GetString("TEST_CLICKHOUSE_URL"),
			PostgresURL:       v.GetString("TEST_POSTGRES_URL"),
			LocalStackURL:     localstackURL,
			GCPURL:            v.GetString("TEST_GCP_URL"),
			AzureSBConnString: v.GetString("TEST_AZURE_SB_CONNSTRING"),
			RabbitMQURL:       rabbitmqURL,
			KafkaURL:          v.GetString("TEST_KAFKA_URL"),
			MockServerURL:     mockServerURL,
			Images:            readImages(v),
		}
		return
	}

	cfg = &Config{
		TestInfra:         v.GetBool("TESTINFRA"),
		TestAzure:         v.GetBool("TESTAZURE"),
		ClickHouseURL:     "",
		PostgresURL:       "",
		LocalStackURL:     "",
		GCPURL:            "",
		AzureSBConnString: "",
		RabbitMQURL:       "",
		KafkaURL:          "",
		MockServerURL:     "",
		Images:            readImages(v),
	}
}

func ReadConfig() *Config {
	cfgSync.Do(initConfig)
	return cfg
}

// Start marks a suite as needing test infrastructure and returns the func to
// defer at the end of it.
//
// The containers started for the TESTINFRA-unset path are shared by every suite
// in a test binary and deliberately outlive all of them: they are torn down by
// the testcontainers reaper when the process exits. Stopping them when a suite
// finishes does not work, because the sync.Once guarding each container has
// already fired — the next suite would reuse an endpoint with nothing behind it.
func Start(t *testing.T) func() {
	testutil.CheckIntegrationTest(t)
	return func() {}
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
