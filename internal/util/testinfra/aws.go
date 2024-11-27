package testinfra

import (
	"context"
	"log"
	"strings"
	"sync"
	"testing"

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

var localstackOnce sync.Once

func EnsureLocalStack() string {
	cfg := ReadConfig()
	if cfg.LocalStackURL == "" {
		localstackOnce.Do(func() {
			startLocalStackTestContainer(cfg)
		})
	}
	return cfg.LocalStackURL
}

func startLocalStackTestContainer(cfg *Config) {
	ctx := context.Background()

	localstackContainer, err := localstack.Run(ctx,
		"localstack/localstack:latest",
	)

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
	cfg.cleanupFns = append(cfg.cleanupFns, func() {
		if err := localstackContainer.Terminate(ctx); err != nil {
			log.Println("Failed to terminate localstack container", err)
		}
	})
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
