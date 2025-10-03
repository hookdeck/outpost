package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

// local
const (
	TOPIC_NAME        = "destination-test"
	SUBSCRIPTION_NAME = "destination-test-sub"
	CONNECTION_STRING = "Endpoint=sb://localhost;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true;"
)

func main() {
	if err := run(); err != nil {
		panic(err)
	}
}

func run() error {
	// Create client
	client, err := azservicebus.NewClientFromConnectionString(CONNECTION_STRING, nil)
	if err != nil {
		return fmt.Errorf("failed to create client: %w", err)
	}
	defer client.Close(context.Background())

	// Create receiver for the subscription
	receiver, err := client.NewReceiverForSubscription(TOPIC_NAME, SUBSCRIPTION_NAME, nil)
	if err != nil {
		return fmt.Errorf("failed to create receiver: %w", err)
	}
	defer receiver.Close(context.Background())

	// Set up signal handling for graceful shutdown
	termChan := make(chan os.Signal, 1)
	signal.Notify(termChan, syscall.SIGINT, syscall.SIGTERM)

	// Start consuming messages
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		for {
			messages, err := receiver.ReceiveMessages(ctx, 1, nil)
			if err != nil {
				if ctx.Err() != nil {
					// Context cancelled, exit gracefully
					return
				}
				log.Printf("[x] Error receiving messages: %v", err)
				continue
			}

			for _, msg := range messages {
				// Log message details
				log.Printf("[x] Received message:")
				log.Printf("    Message ID: %s", msg.MessageID)
				log.Printf("    Sequence Number: %d", *msg.SequenceNumber)

				// Log application properties (metadata)
				if len(msg.ApplicationProperties) > 0 {
					log.Printf("    Metadata:")
					for k, v := range msg.ApplicationProperties {
						log.Printf("      %s: %v", k, v)
					}
				}

				// Log message body
				var data interface{}
				if err := json.Unmarshal(msg.Body, &data); err == nil {
					// Pretty print JSON
					pretty, _ := json.MarshalIndent(data, "    ", "  ")
					log.Printf("    Body (JSON):\n    %s", string(pretty))
				} else {
					// Raw body
					log.Printf("    Body (Raw): %s", string(msg.Body))
				}

				// Complete the message
				if err := receiver.CompleteMessage(ctx, msg, nil); err != nil {
					log.Printf("[x] Error completing message: %v", err)
				}
			}
		}
	}()

	// Log configuration
	log.Printf("[*] Azure Service Bus Consumer")
	log.Printf("[*] Topic: %s", TOPIC_NAME)
	log.Printf("[*] Subscription: %s", SUBSCRIPTION_NAME)
	log.Printf("[*] Namespace: %s", extractNamespace(CONNECTION_STRING))
	log.Printf("[*] Ready to receive messages. Press Ctrl+C to exit.")

	// Wait for termination signal
	<-termChan
	log.Printf("[*] Shutting down...")
	cancel()

	return nil
}

func extractNamespace(connStr string) string {
	// Simple extraction for display purposes
	start := len("Endpoint=sb://")
	if len(connStr) > start {
		end := start
		for end < len(connStr) && connStr[end] != '.' {
			end++
		}
		if end > start {
			return connStr[start:end]
		}
	}
	return "unknown"
}
