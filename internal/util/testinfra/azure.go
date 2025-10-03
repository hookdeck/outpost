// Azure Service Bus Testing Infrastructure

// IMPORTANT: Azure Service Bus emulator has significant limitations compared to other MQS testing approaches.

// Unlike other MQs (AWS, GCP, RabbitMQ, etc.) which can dynamically provision and teardown
// resources for each test run, the Azure Service Bus emulator does NOT support resource management
// operations. This means:

// 1. We CANNOT create topics, subscriptions, or queues programmatically during tests
// 2. We CANNOT delete or clean up resources after tests complete
// 3. All required resources MUST be pre-created in the emulator before running tests

// Testing Pattern Required:
// Instead of the dynamic provisioning pattern used by other MQS systems:
//   - AWS: NewMQAWSConfig() creates unique topics/queues per test
//   - RabbitMQ: NewMQRabbitMQConfig() creates unique exchanges/queues per test

// Azure MUST use a static resource allocation pattern:
//   - GetAzureSBMQConfig(t, "TestName") returns pre-configured resources for that specific test
//   - Each test must have dedicated, pre-existing resources in the emulator

// Required Pre-Created Resources:
// The following resources must exist in your Azure Service Bus emulator before running tests:

// For TestIntegrationMQ_AzureServiceBus:
//   - Topic: TestIntegrationMQ_AzureServiceBus-topic
//   - Subscription: TestIntegrationMQ_AzureServiceBus-subscription

// For any future tests, follow the naming pattern:
//   - Topic: {TestName}-topic
//   - Subscription: {TestName}-subscription

// Example Usage:
//   // Instead of:
//   config := testinfra.NewMQAzureSBConfig(t, nil)

//   // Use:
//   config := testinfra.GetMQAzureSBConfig(t, "TestIntegrationMQ_AzureServiceBus")

// This approach ensures test isolation while working within the Azure emulator's constraints.
// Each test gets its own dedicated resources that won't interfere with other tests.

package testinfra

import (
	"testing"

	"github.com/hookdeck/outpost/internal/mqs"
)

func GetAzureSBConnString() string {
	cfg := ReadConfig()
	if !cfg.TestAzure {
		return ""
	}
	return cfg.AzureSBConnString
}

func GetMQAzureConfig(t *testing.T, testName string) mqs.QueueConfig {
	connString := GetAzureSBConnString()
	if connString == "" {
		t.Skip("Test suite is not configured to run Azure tests")
		return mqs.QueueConfig{}
	}

	azureSBMap := map[string]mqs.QueueConfig{
		"TestIntegrationMQ_AzureServiceBus": {
			AzureServiceBus: &mqs.AzureServiceBusConfig{
				ConnectionString: connString,
				Topic:            "TestIntegrationMQ_AzureServiceBus-topic",
				Subscription:     "TestIntegrationMQ_AzureServiceBus-subscription",
			},
		},
		"TestDestinationAzureServiceBusSuite": {
			AzureServiceBus: &mqs.AzureServiceBusConfig{
				ConnectionString: connString,
				Topic:            "TestDestinationAzureServiceBusSuite-topic",
				Subscription:     "TestDestinationAzureServiceBusSuite-subscription",
			},
		},
	}

	if cfg, ok := azureSBMap[testName]; !ok {
		t.Fatalf("Test name %s is not configured for Azure Service Bus", testName)
		return mqs.QueueConfig{}
	} else {
		return cfg
	}
}
