package main

import (
	"context"
	"fmt"
	"log"
	"os"

	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"github.com/manifoldco/promptui"
)

func runCreateDestinationExample() {
	adminAPIKey := os.Getenv("ADMIN_API_KEY")
	if adminAPIKey == "" {
		log.Fatal("Please set the ADMIN_API_KEY environment variable.")
	}

	tenantID := os.Getenv("TENANT_ID")
	if tenantID == "" {
		tenantID = "hookdeck"
	}

	apiServerURL := os.Getenv("API_BASE_URL")
	if apiServerURL == "" {
		serverURL := os.Getenv("OUTPOST_URL")
		if serverURL == "" {
			serverURL = os.Getenv("SERVER_URL")
		}
		if serverURL == "" {
			serverURL = "http://localhost:3333"
		}
		apiServerURL = fmt.Sprintf("%s/api/v1", serverURL)
	}

	client := outpostgo.New(
		outpostgo.WithSecurity(adminAPIKey),
		outpostgo.WithServerURL(apiServerURL),
	)

	_, err := client.Tenants.Upsert(context.Background(), tenantID, nil)

	if err != nil {
		log.Fatalf("Error upserting tenant: %v", err)
	}

	fmt.Print(`
You can create a topic or queue specific connection string with send-only permissions using the Azure CLI.
Please replace $RESOURCE_GROUP, $NAMESPACE_NAME, and $TOPIC_NAME with your actual values.

Create a send-only policy for the topic:
az servicebus topic authorization-rule create \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --topic-name $TOPIC_NAME \
  --name SendOnlyPolicy \
  --rights Send

or for a queue:

az servicebus queue authorization-rule create \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --queue-name $QUEUE_NAME \
  --name SendOnlyPolicy \
  --rights Send

Get the Topic-Specific Connection String:
az servicebus topic authorization-rule keys list \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --topic-name $TOPIC_NAME \
  --name SendOnlyPolicy \
  --query primaryConnectionString \
  --output tsv

or for a Queue-Specific Connection String:
az servicebus queue authorization-rule keys list \
  --resource-group $RESOURCE_GROUP \
  --namespace-name $NAMESPACE_NAME \
  --queue-name $QUEUE_NAME \
  --name SendOnlyPolicy \
  --query primaryConnectionString \
  --output tsv
`)
	fmt.Println()

	promptConnectionString := promptui.Prompt{
		Label: "Enter Azure Service Bus Connection String",
	}
	connectionString, err := promptConnectionString.Run()
	if err != nil {
		log.Fatalf("Prompt failed %v\n", err)
	}

	promptTopicOrQueue := promptui.Prompt{
		Label: "Enter Azure Service Bus Topic or Queue name",
	}
	topicOrQueueName, err := promptTopicOrQueue.Run()
	if err != nil {
		log.Fatalf("Prompt failed %v\n", err)
	}

	topics := components.CreateTopicsTopicsEnum(components.TopicsEnumWildcard)

	destination := components.CreateDestinationCreateAzureServicebus(
		components.DestinationCreateAzureServiceBus{
			Topics: topics,
			Config: components.AzureServiceBusConfig{
				Name: topicOrQueueName,
			},
			Credentials: components.AzureServiceBusCredentials{
				ConnectionString: connectionString,
			},
		},
	)

	createRes, err := client.Destinations.Create(context.Background(), tenantID, destination)
	if err != nil {
		log.Fatalf("Error creating destination: %v", err)
	}

	if createRes.Destination != nil {
		fmt.Println("Destination created successfully:")
		// Using a simple print for brevity, a real application might use JSON marshalling
		fmt.Printf("  ID: %s\n", createRes.Destination.DestinationAzureServiceBus.ID)
		fmt.Printf("  Name: %s\n", createRes.Destination.DestinationAzureServiceBus.Config.Name)
		fmt.Printf("  Type: %s\n", createRes.Destination.Type)
	} else {
		fmt.Println("Destination creation did not return a destination object.")
	}
}
