package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
)

const (
	AzureServiceBusConnectionString = "Endpoint=sb://localhost;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=SAS_KEY_VALUE;UseDevelopmentEmulator=true;"
	AzureServiceBusTopic            = "outpost-publish"
	AzureServiceBusSubscription     = "outpost-publish-sub"
)

func publishAzureServiceBus(body map[string]interface{}) error {
	log.Printf("[x] Publishing Azure Service Bus")

	ctx := context.Background()
	client, err := azservicebus.NewClientFromConnectionString(AzureServiceBusConnectionString, nil)
	if err != nil {
		return fmt.Errorf("failed to create Azure Service Bus client: %w", err)
	}
	defer client.Close(ctx)

	sender, err := client.NewSender(AzureServiceBusTopic, nil)
	if err != nil {
		return fmt.Errorf("failed to create sender: %w", err)
	}
	defer sender.Close(ctx)

	messageBody, err := json.Marshal(body)
	if err != nil {
		return err
	}

	message := &azservicebus.Message{
		Body: messageBody,
		ApplicationProperties: map[string]interface{}{
			"source": "outpost-publish",
		},
	}

	err = sender.SendMessage(ctx, message, nil)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	log.Printf("[x] Published message to Azure Service Bus topic %s", AzureServiceBusTopic)
	return nil
}

func declareAzureServiceBus() error {
	log.Printf("[*] Declaring Azure Service Bus Publish infra")
	return fmt.Errorf("azure sb emulator does not support declaring topics and subscriptions. Use `%s` and `%s` for the publishmq topic and subscription", AzureServiceBusTopic, AzureServiceBusSubscription)
}
