package mqinfra

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/servicebus/armservicebus"
)

type infraAzureServiceBus struct {
	cfg *MQInfraConfig
}

func (infra *infraAzureServiceBus) Exist(ctx context.Context) (bool, error) {
	if infra.cfg == nil || infra.cfg.AzureServiceBus == nil {
		return false, fmt.Errorf("failed assertion: cfg.AzureServiceBus != nil")
	}

	cfg := infra.cfg.AzureServiceBus

	if cfg.ConnectionString != "" {
		return true, nil
	}

	// Create credential for authentication
	cred, err := azidentity.NewClientSecretCredential(
		cfg.TenantID,
		cfg.ClientID,
		cfg.ClientSecret,
		nil,
	)
	if err != nil {
		return false, fmt.Errorf("failed to create credential: %w", err)
	}

	// Create clients for topic and subscription management
	topicClient, err := armservicebus.NewTopicsClient(cfg.SubscriptionID, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create topic client: %w", err)
	}

	subClient, err := armservicebus.NewSubscriptionsClient(cfg.SubscriptionID, cred, nil)
	if err != nil {
		return false, fmt.Errorf("failed to create subscription client: %w", err)
	}

	// Check if topic exists
	_, err = topicClient.Get(ctx, cfg.ResourceGroup, cfg.Namespace, cfg.Topic, nil)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check topic existence: %w", err)
	}

	// Check if subscription exists
	_, err = subClient.Get(ctx, cfg.ResourceGroup, cfg.Namespace, cfg.Topic, cfg.Subscription, nil)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, fmt.Errorf("failed to check subscription existence: %w", err)
	}

	return true, nil
}

func (infra *infraAzureServiceBus) Declare(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.AzureServiceBus == nil {
		return fmt.Errorf("failed assertion: cfg.AzureServiceBus != nil")
	}

	cfg := infra.cfg.AzureServiceBus

	if cfg.ConnectionString != "" {
		return nil
	}

	// Create credential for authentication
	cred, err := azidentity.NewClientSecretCredential(
		cfg.TenantID,
		cfg.ClientID,
		cfg.ClientSecret,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create credential: %w", err)
	}

	// Create clients for topic and subscription management
	topicClient, err := armservicebus.NewTopicsClient(cfg.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create topic client: %w", err)
	}

	subClient, err := armservicebus.NewSubscriptionsClient(cfg.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create subscription client: %w", err)
	}

	// Declare main topic (upsert)
	topicName := cfg.Topic
	err = infra.declareTopic(ctx, topicClient, cfg.ResourceGroup, cfg.Namespace, topicName)
	if err != nil {
		return fmt.Errorf("failed to declare topic: %w", err)
	}

	// Configure retry policy settings
	lockDuration := "PT1M" // 1 minute default
	if infra.cfg.Policy.VisibilityTimeout > 0 {
		lockDuration = fmt.Sprintf("PT%dS", infra.cfg.Policy.VisibilityTimeout)
	}

	// Set maximum delivery count (Azure default is 10, but we apply our retry policy)
	maxDeliveryCount := int32(10) // Azure default
	if infra.cfg.Policy.RetryLimit > 0 {
		// Adding 1 because Azure counts the initial delivery as attempt #1
		maxDeliveryCount = int32(infra.cfg.Policy.RetryLimit + 1)
		if maxDeliveryCount < 1 {
			maxDeliveryCount = 1
		}
	}

	// Create main subscription with DLQ configuration
	subName := cfg.Subscription
	subConfig := &armservicebus.SBSubscription{
		Properties: &armservicebus.SBSubscriptionProperties{
			LockDuration:                     to.Ptr(lockDuration),
			DefaultMessageTimeToLive:         to.Ptr("P14D"), // 14 days
			DeadLetteringOnMessageExpiration: to.Ptr(true),
			MaxDeliveryCount:                 to.Ptr(maxDeliveryCount),
			EnableBatchedOperations:          to.Ptr(true),
			RequiresSession:                  to.Ptr(false),
			// Azure Service Bus has built-in dead letter queue functionality
			// Dead lettered messages automatically go to the subscription's DLQ
		},
	}

	err = infra.declareSubscription(ctx, subClient, cfg.ResourceGroup, cfg.Namespace, topicName, subName, subConfig)
	if err != nil {
		return fmt.Errorf("failed to declare subscription: %w", err)
	}

	return nil
}

func (infra *infraAzureServiceBus) TearDown(ctx context.Context) error {
	if infra.cfg == nil || infra.cfg.AzureServiceBus == nil {
		return fmt.Errorf("failed assertion: cfg.AzureServiceBus != nil")
	}

	cfg := infra.cfg.AzureServiceBus

	if cfg.ConnectionString != "" {
		return nil
	}

	// Create credential for authentication
	cred, err := azidentity.NewClientSecretCredential(
		cfg.TenantID,
		cfg.ClientID,
		cfg.ClientSecret,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create credential: %w", err)
	}

	// Create clients for topic and subscription management
	topicClient, err := armservicebus.NewTopicsClient(cfg.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create topic client: %w", err)
	}

	subClient, err := armservicebus.NewSubscriptionsClient(cfg.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("failed to create subscription client: %w", err)
	}

	topicName := cfg.Topic

	// Delete main subscription
	err = infra.deleteSubscription(ctx, subClient, cfg.ResourceGroup, cfg.Namespace, topicName, cfg.Subscription)
	if err != nil {
		return fmt.Errorf("failed to delete subscription: %w", err)
	}

	// Delete main topic
	err = infra.deleteTopic(ctx, topicClient, cfg.ResourceGroup, cfg.Namespace, topicName)
	if err != nil {
		return fmt.Errorf("failed to delete topic: %w", err)
	}

	return nil
}

// Helper methods for resource management

func (infra *infraAzureServiceBus) declareTopic(ctx context.Context, client *armservicebus.TopicsClient, resourceGroup, namespace, topicName string) error {
	// First, try to get the existing topic
	_, err := client.Get(ctx, resourceGroup, namespace, topicName, nil)
	if err == nil {
		// Topic already exists, no need to create
		return nil
	}

	// If it's not a "not found" error, return the error
	if !isNotFoundError(err) {
		return fmt.Errorf("failed to check topic %s existence: %w", topicName, err)
	}

	// Topic doesn't exist, create it
	_, err = client.CreateOrUpdate(
		ctx,
		resourceGroup,
		namespace,
		topicName,
		armservicebus.SBTopic{
			Properties: &armservicebus.SBTopicProperties{
				MaxSizeInMegabytes:                  to.Ptr[int32](1024),
				DefaultMessageTimeToLive:            to.Ptr("P14D"), // 14 days
				EnablePartitioning:                  to.Ptr(false),
				RequiresDuplicateDetection:          to.Ptr(false),
				DuplicateDetectionHistoryTimeWindow: to.Ptr("PT10M"), // 10 minutes
				SupportOrdering:                     to.Ptr(true),
			},
		},
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create topic %s: %w", topicName, err)
	}

	return nil
}

func (infra *infraAzureServiceBus) declareSubscription(ctx context.Context, client *armservicebus.SubscriptionsClient, resourceGroup, namespace, topicName, subName string, config *armservicebus.SBSubscription) error {
	// First, try to get the existing subscription
	_, err := client.Get(ctx, resourceGroup, namespace, topicName, subName, nil)
	if err == nil {
		// Subscription already exists, no need to create
		return nil
	}

	// If it's not a "not found" error, return the error
	if !isNotFoundError(err) {
		return fmt.Errorf("failed to check subscription %s existence: %w", subName, err)
	}

	// Subscription doesn't exist, create it
	_, err = client.CreateOrUpdate(
		ctx,
		resourceGroup,
		namespace,
		topicName,
		subName,
		*config,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to create subscription %s: %w", subName, err)
	}

	return nil
}

func (infra *infraAzureServiceBus) deleteTopic(ctx context.Context, client *armservicebus.TopicsClient, resourceGroup, namespace, topicName string) error {
	// Delete topic directly - no existence check needed
	_, err := client.Delete(ctx, resourceGroup, namespace, topicName, nil)
	if err != nil {
		// If topic doesn't exist, that's fine - deletion is idempotent
		if isNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete topic %s: %w", topicName, err)
	}

	return nil
}

func (infra *infraAzureServiceBus) deleteSubscription(ctx context.Context, client *armservicebus.SubscriptionsClient, resourceGroup, namespace, topicName, subName string) error {
	// Delete subscription directly - no existence check needed
	_, err := client.Delete(ctx, resourceGroup, namespace, topicName, subName, nil)
	if err != nil {
		// If subscription doesn't exist, that's fine - deletion is idempotent
		if isNotFoundError(err) {
			return nil
		}
		return fmt.Errorf("failed to delete subscription %s: %w", subName, err)
	}

	return nil
}

// Helper function to check if error is a "not found" error
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}

	// Check for common Azure "not found" error patterns
	errStr := err.Error()

	// HTTP 404 Not Found
	if strings.Contains(errStr, "404") || strings.Contains(errStr, "Not Found") {
		return true
	}

	// Azure-specific error codes
	if strings.Contains(errStr, "ResourceNotFound") ||
		strings.Contains(errStr, "EntityNotFound") ||
		strings.Contains(errStr, "MessagingEntityNotFound") {
		return true
	}

	return false
}
