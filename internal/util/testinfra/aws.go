package testinfra

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/hookdeck/outpost/internal/util/awsutil"
	"github.com/testcontainers/testcontainers-go/modules/localstack"
)

func NewMQAWSConfig(t *testing.T, attributes map[string]string) mqs.QueueConfig {
	queueConfig := mqs.QueueConfig{
		AWSSQS: &mqs.AWSSQSConfig{
			Endpoint:                  EnsureLocalStack(),
			Region:                    "us-east-1",
			ServiceAccountCredentials: "test:test:",
			Topic:                     uuid.New().String(),
			WaitTime:                  1 * time.Second, // Short wait for tests
		},
	}
	ctx := context.Background()
	if _, err := DeclareTestAWSInfrastructure(ctx, queueConfig.AWSSQS, attributes); err != nil {
		panic(err)
	}
	t.Cleanup(func() {
		if err := TeardownTestAWSInfrastructure(ctx, queueConfig.AWSSQS); err != nil {
			log.Println("Failed to teardown AWS infrastructure", err, *queueConfig.AWSSQS)
		}
	})
	return queueConfig
}

var (
	localstackOnce      sync.Once
	localstackReadyOnce sync.Once
)

func EnsureLocalStack() string {
	cfg := ReadConfig()
	if cfg.LocalStackURL == "" {
		localstackOnce.Do(func() {
			startLocalStackTestContainer(cfg)
		})
	}
	// LocalStack serves HTTP before its individual services are usable, so ask
	// the health endpoint rather than settling for an open port.
	localstackReadyOnce.Do(func() {
		waitReadyLogged("localstack", cfg.LocalStackURL, func() error {
			req, err := http.NewRequest(http.MethodGet, cfg.LocalStackURL+"/_localstack/health", nil)
			if err != nil {
				return err
			}
			resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("health returned %s", resp.Status)
			}
			return nil
		})
	})
	return cfg.LocalStackURL
}

func startLocalStackTestContainer(cfg *Config) {
	ctx := context.Background()

	localstackContainer, err := localstack.Run(ctx, cfg.Images.LocalStack)

	if err != nil {
		panic(err)
	}

	endpoint, err := localstackContainer.PortEndpoint(ctx, "4566/tcp", "")
	if err != nil {
		panic(err)
	}
	if !strings.Contains(endpoint, "http://") {
		endpoint = "http://" + endpoint
	}
	log.Printf("Localstack running at %s", endpoint)
	cfg.LocalStackURL = endpoint
}

func DeclareTestAWSInfrastructure(ctx context.Context, cfg *mqs.AWSSQSConfig, attributes map[string]string) (string, error) {
	sqsClient, err := awsutil.SQSClientFromConfig(ctx, cfg)
	if err != nil {
		return "", err
	}
	queueURL, err := awsutil.EnsureQueue(ctx, sqsClient, cfg.Topic, awsutil.MakeCreateQueue(attributes))
	if err != nil {
		return "", err
	}
	return queueURL, nil
}

func TeardownTestAWSInfrastructure(ctx context.Context, cfg *mqs.AWSSQSConfig) error {
	sqsClient, err := awsutil.SQSClientFromConfig(ctx, cfg)
	if err != nil {
		return err
	}
	queueURL, err := awsutil.EnsureQueue(ctx, sqsClient, cfg.Topic, nil)
	if err != nil {
		return err
	}
	return awsutil.DeleteQueue(ctx, sqsClient, queueURL)
}
