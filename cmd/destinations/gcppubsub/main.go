package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cloud.google.com/go/pubsub"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	// Default configuration for local emulator
	DEFAULT_PROJECT_ID   = "test-project"
	DEFAULT_TOPIC        = "test-topic"
	DEFAULT_SUBSCRIPTION = "test-subscription"
	DEFAULT_ENDPOINT     = "localhost:8085" // Default emulator endpoint

	// To use real GCP, set these environment variables:
	// GCP_PROJECT_ID - Your GCP project ID
	// GCP_TOPIC - Your Pub/Sub topic name
	// GCP_SUBSCRIPTION - Your subscription name
	// GCP_CREDENTIALS - Path to service account JSON file
	// GCP_ENDPOINT - Leave empty for production or set for custom endpoint
)

func main() {
	// Check for command arguments
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "-h", "--help":
			printHelp()
			return
		case "clean", "cleanup":
			if err := cleanup(); err != nil {
				log.Fatalf("Cleanup error: %v", err)
			}
			return
		}
	}

	if err := run(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func printHelp() {
	fmt.Println(`GCP Pub/Sub Test Consumer

This program connects to GCP Pub/Sub and listens for messages on a subscription.
It supports both the local emulator and real GCP environments.

USAGE:
  go run cmd/destinations/gcppubsub/main.go [command]

COMMANDS:
  help     Show this help message
  clean    Delete the topic and subscription (cleanup)

DEFAULT CONFIGURATION (Emulator):
  - Project ID: test-project
  - Topic: test-topic
  - Subscription: test-subscription
  - Endpoint: localhost:8085

TO USE WITH LOCAL EMULATOR:
  # Make sure the emulator is running, then:
  go run cmd/destinations/gcppubsub/main.go

TO USE WITH REAL GCP:
  export GCP_PROJECT_ID="your-project-id"
  export GCP_TOPIC="your-topic"
  export GCP_SUBSCRIPTION="your-subscription"
  export GCP_CREDENTIALS="/path/to/service-account.json"
  export GCP_ENDPOINT=""  # Leave empty for production
  go run cmd/destinations/gcppubsub/main.go

ENVIRONMENT VARIABLES:
  GCP_PROJECT_ID     - GCP project ID (default: test-project)
  GCP_TOPIC          - Pub/Sub topic name (default: test-topic)
  GCP_SUBSCRIPTION   - Subscription name (default: test-subscription)
  GCP_CREDENTIALS    - Path to service account JSON file (default: none, uses emulator)
  GCP_ENDPOINT       - Custom endpoint (default: localhost:8085)

NOTES:
  - The program will create the topic and subscription if they don't exist
  - Messages are automatically acknowledged after processing
  - Use CTRL+C to gracefully shut down`)
}

func run() error {
	ctx := context.Background()

	// Get configuration from environment or use defaults
	projectID := getEnvOrDefault("GCP_PROJECT_ID", DEFAULT_PROJECT_ID)
	topicName := getEnvOrDefault("GCP_TOPIC", DEFAULT_TOPIC)
	subscriptionName := getEnvOrDefault("GCP_SUBSCRIPTION", DEFAULT_SUBSCRIPTION)
	endpoint := getEnvOrDefault("GCP_ENDPOINT", DEFAULT_ENDPOINT)
	credentialsPath := os.Getenv("GCP_CREDENTIALS")

	log.Printf("Configuration:")
	log.Printf("  Project ID: %s", projectID)
	log.Printf("  Topic: %s", topicName)
	log.Printf("  Subscription: %s", subscriptionName)
	log.Printf("  Endpoint: %s", endpoint)
	if credentialsPath != "" {
		log.Printf("  Credentials: %s", credentialsPath)
	} else {
		log.Printf("  Credentials: Using emulator (no auth)")
	}

	// Create client options
	var opts []option.ClientOption
	if endpoint != "" {
		// Using emulator or custom endpoint
		log.Printf("Connecting to emulator/custom endpoint: %s", endpoint)
		opts = append(opts,
			option.WithEndpoint(endpoint),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	} else if credentialsPath != "" {
		// Using real GCP with service account
		log.Printf("Using service account credentials from: %s", credentialsPath)
		opts = append(opts, option.WithCredentialsFile(credentialsPath))
	}

	// Create client
	client, err := pubsub.NewClient(ctx, projectID, opts...)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	// Get or create topic
	topic := client.Topic(topicName)
	exists, err := topic.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check topic existence: %w", err)
	}
	if !exists {
		log.Printf("Topic %s doesn't exist, creating...", topicName)
		topic, err = client.CreateTopic(ctx, topicName)
		if err != nil {
			return fmt.Errorf("failed to create topic: %w", err)
		}
		log.Printf("Created topic: %s", topicName)
	} else {
		log.Printf("Using existing topic: %s", topicName)
	}

	// Get or create subscription
	sub := client.Subscription(subscriptionName)
	exists, err = sub.Exists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check subscription existence: %w", err)
	}
	if !exists {
		log.Printf("Subscription %s doesn't exist, creating...", subscriptionName)
		sub, err = client.CreateSubscription(ctx, subscriptionName, pubsub.SubscriptionConfig{
			Topic:       topic,
			AckDeadline: 10 * time.Second,
		})
		if err != nil {
			return fmt.Errorf("failed to create subscription: %w", err)
		}
		log.Printf("Created subscription: %s", subscriptionName)
	} else {
		log.Printf("Using existing subscription: %s", subscriptionName)
	}

	// Set up signal handling
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	// Start receiving messages
	log.Printf("[*] Waiting for messages. To exit press CTRL+C")

	// Create a cancellable context for receiving
	receiveCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Handle messages
	err = sub.Receive(receiveCtx, func(ctx context.Context, msg *pubsub.Message) {
		log.Printf("[x] Received message ID: %s", msg.ID)
		log.Printf("    Data: %s", string(msg.Data))

		// Pretty print attributes
		if len(msg.Attributes) > 0 {
			log.Printf("    Attributes:")
			for key, value := range msg.Attributes {
				log.Printf("      %s: %s", key, value)
			}
		}

		// Try to parse as JSON for prettier output
		var jsonData interface{}
		if err := json.Unmarshal(msg.Data, &jsonData); err == nil {
			prettyJSON, _ := json.MarshalIndent(jsonData, "    ", "  ")
			log.Printf("    JSON Data:\n    %s", string(prettyJSON))
		}

		// Acknowledge the message
		msg.Ack()
	})

	if err != nil {
		return fmt.Errorf("receive error: %w", err)
	}

	// Wait for termination signal
	<-termChan
	log.Println("Shutting down...")
	cancel()

	return nil
}

func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func cleanup() error {
	ctx := context.Background()

	// Get configuration from environment or use defaults
	projectID := getEnvOrDefault("GCP_PROJECT_ID", DEFAULT_PROJECT_ID)
	topicName := getEnvOrDefault("GCP_TOPIC", DEFAULT_TOPIC)
	subscriptionName := getEnvOrDefault("GCP_SUBSCRIPTION", DEFAULT_SUBSCRIPTION)
	endpoint := getEnvOrDefault("GCP_ENDPOINT", DEFAULT_ENDPOINT)
	credentialsPath := os.Getenv("GCP_CREDENTIALS")

	log.Printf("Cleanup Configuration:")
	log.Printf("  Project ID: %s", projectID)
	log.Printf("  Topic: %s", topicName)
	log.Printf("  Subscription: %s", subscriptionName)
	log.Printf("  Endpoint: %s", endpoint)

	// Create client options
	var opts []option.ClientOption
	if endpoint != "" {
		opts = append(opts,
			option.WithEndpoint(endpoint),
			option.WithoutAuthentication(),
			option.WithGRPCDialOption(grpc.WithTransportCredentials(insecure.NewCredentials())),
		)
	} else if credentialsPath != "" {
		opts = append(opts, option.WithCredentialsFile(credentialsPath))
	}

	// Create client
	client, err := pubsub.NewClient(ctx, projectID, opts...)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close()

	// Delete subscription first
	sub := client.Subscription(subscriptionName)
	exists, err := sub.Exists(ctx)
	if err != nil {
		log.Printf("Warning: Failed to check subscription existence: %v", err)
	} else if exists {
		if err := sub.Delete(ctx); err != nil {
			log.Printf("Warning: Failed to delete subscription %s: %v", subscriptionName, err)
		} else {
			log.Printf("Deleted subscription: %s", subscriptionName)
		}
	} else {
		log.Printf("Subscription %s doesn't exist", subscriptionName)
	}

	// Delete topic
	topic := client.Topic(topicName)
	exists, err = topic.Exists(ctx)
	if err != nil {
		log.Printf("Warning: Failed to check topic existence: %v", err)
	} else if exists {
		if err := topic.Delete(ctx); err != nil {
			log.Printf("Warning: Failed to delete topic %s: %v", topicName, err)
		} else {
			log.Printf("Deleted topic: %s", topicName)
		}
	} else {
		log.Printf("Topic %s doesn't exist", topicName)
	}

	log.Println("Cleanup completed")
	return nil
}
