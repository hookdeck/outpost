package testinfra

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/google/uuid"
	"github.com/hookdeck/outpost/internal/mqs"
	"github.com/testcontainers/testcontainers-go/modules/gcloud"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewMQGCPConfig(t *testing.T, attributes map[string]string) mqs.QueueConfig {
	queueConfig := mqs.QueueConfig{
		GCPPubSub: &mqs.GCPPubSubConfig{
			ProjectID:                 "test-project",
			TopicID:                   fmt.Sprintf("test-topic-%s", uuid.New().String()),
			SubscriptionID:            fmt.Sprintf("test-sub-%s", uuid.New().String()),
			ServiceAccountCredentials: "",
		},
	}
	ctx := context.Background()
	url := EnsureGCP(t)
	if err := DeclareTestGCPInfrastructure(ctx, queueConfig.GCPPubSub, url); err != nil {
		panic(err)
	}
	t.Cleanup(func() {
		if err := TeardownTestGCPInfrastructure(ctx, queueConfig.GCPPubSub, url); err != nil {
			log.Println("Failed to teardown GCP infrastructure", err, *queueConfig.GCPPubSub)
		}
	})
	return queueConfig
}

var gcpService = &service{name: "pubsub emulator", startHint: hintDest}

// EnsureGCP returns the host:port of the test Pub/Sub emulator and points
// PUBSUB_EMULATOR_HOST at it, failing t if it isn't available.
func EnsureGCP(t testing.TB) string {
	t.Helper()
	cfg := ReadConfig()
	endpoint := gcpService.ensure(t, cfg.GCPURL,
		func() (string, error) { return startGCPTestContainer(cfg) },
		dialTCP)
	os.Setenv("PUBSUB_EMULATOR_HOST", endpoint)
	return endpoint
}

func startGCPTestContainer(cfg *Config) (string, error) {
	ctx := context.Background()

	gcloudContainer, err := gcloud.RunPubsub(ctx, cfg.Images.GCP)
	if err != nil {
		return "", err
	}

	endpoint, err := gcloudContainer.PortEndpoint(ctx, "8085/tcp", "")
	if err != nil {
		return "", err
	}
	log.Printf("GCP Emulator running at %s", endpoint)
	return endpoint, nil
}

func DeclareTestGCPInfrastructure(ctx context.Context, cfg *mqs.GCPPubSubConfig, endpoint string) error {
	// Create client with emulator configuration
	client, err := pubsub.NewClient(ctx, cfg.ProjectID,
		option.WithEndpoint(endpoint),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		return fmt.Errorf("failed to create pubsub client: %v", err)
	}
	defer client.Close()

	// Check if topic exists
	topic := client.Topic(cfg.TopicID)
	exists, err := topic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if topic exists: %v", err)
	}

	// Create topic if it doesn't exist
	if !exists {
		topic, err = client.CreateTopic(ctx, cfg.TopicID)
		if err != nil {
			return fmt.Errorf("failed to create topic: %v", err)
		}
		log.Printf("Created topic: %s", cfg.TopicID)
	}

	// Create subscription if needed
	sub := client.Subscription(cfg.SubscriptionID)
	exists, err = sub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if subscription exists: %v", err)
	}

	if !exists {
		_, err = client.CreateSubscription(ctx, cfg.SubscriptionID, pubsub.SubscriptionConfig{
			Topic:       topic,
			AckDeadline: 20 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("failed to create subscription: %v", err)
		}
		log.Printf("Created subscription: %s", cfg.SubscriptionID)
	}

	return nil
}

func TeardownTestGCPInfrastructure(ctx context.Context, cfg *mqs.GCPPubSubConfig, endpoint string) error {
	// Create client with emulator configuration
	client, err := pubsub.NewClient(ctx, cfg.ProjectID,
		option.WithEndpoint(endpoint),
		option.WithoutAuthentication(),
		option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())))
	if err != nil {
		return fmt.Errorf("failed to create pubsub client: %v", err)
	}
	defer client.Close()

	// Delete subscription if it exists
	sub := client.Subscription(cfg.SubscriptionID)
	exists, err := sub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if subscription exists: %v", err)
	}

	if exists {
		err = sub.Delete(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete subscription: %v", err)
		}
		log.Printf("Deleted subscription: %s", cfg.SubscriptionID)
	}

	// Delete topic if it exists
	topic := client.Topic(cfg.TopicID)
	exists, err = topic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check if topic exists: %v", err)
	}

	if exists {
		err = topic.Delete(ctx)
		if err != nil {
			return fmt.Errorf("failed to delete topic: %v", err)
		}
		log.Printf("Deleted topic: %s", cfg.TopicID)
	}

	return nil
}
