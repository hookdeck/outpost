package config

import (
	"context"

	"github.com/hookdeck/outpost/internal/mqinfra"
	"github.com/hookdeck/outpost/internal/mqs"
)

type AzureServiceBusConfig struct {
	// Using AzureSB with ConnectionString will skip infra management
	ConnectionString string `yaml:"connection_string" env:"AZURE_SERVICEBUS_CONNECTION_STRING" desc:"Azure Service Bus connection string" required:"N"`

	TenantID       string `yaml:"tenant_id" env:"AZURE_SERVICEBUS_TENANT_ID" desc:"Azure Active Directory tenant ID" required:"Y"`
	ClientID       string `yaml:"client_id" env:"AZURE_SERVICEBUS_CLIENT_ID" desc:"Service principal client ID" required:"Y"`
	ClientSecret   string `yaml:"client_secret" env:"AZURE_SERVICEBUS_CLIENT_SECRET" desc:"Service principal client secret" required:"Y"`
	SubscriptionID string `yaml:"subscription_id" env:"AZURE_SERVICEBUS_SUBSCRIPTION_ID" desc:"Azure subscription ID" required:"Y"`
	ResourceGroup  string `yaml:"resource_group" env:"AZURE_SERVICEBUS_RESOURCE_GROUP" desc:"Azure resource group name" required:"Y"`
	Namespace      string `yaml:"namespace" env:"AZURE_SERVICEBUS_NAMESPACE" desc:"Azure Service Bus namespace" required:"Y"`

	DeliveryTopic        string `yaml:"delivery_topic" env:"AZURE_SERVICEBUS_DELIVERY_TOPIC" desc:"Topic name for delivery queue" required:"N" default:"outpost-delivery"`
	DeliverySubscription string `yaml:"delivery_subscription" env:"AZURE_SERVICEBUS_DELIVERY_SUBSCRIPTION" desc:"Subscription name for delivery queue" required:"N" default:"outpost-delivery-sub"`
	LogTopic             string `yaml:"log_topic" env:"AZURE_SERVICEBUS_LOG_TOPIC" desc:"Topic name for log queue" required:"N" default:"outpost-log"`
	LogSubscription      string `yaml:"log_subscription" env:"AZURE_SERVICEBUS_LOG_SUBSCRIPTION" desc:"Subscription name for log queue" required:"N" default:"outpost-log-sub"`

	// connectionStringOnce  sync.Once
	// connectionString      string
	// connectionStringError error
}

func (c *AzureServiceBusConfig) IsConfigured() bool {
	return c.ConnectionString != "" || (c.TenantID != "" && c.ClientID != "" && c.ClientSecret != "" && c.SubscriptionID != "" && c.ResourceGroup != "" && c.Namespace != "")
}

func (c *AzureServiceBusConfig) GetProviderType() string {
	return "azure_service_bus"
}

func (c *AzureServiceBusConfig) getTopicByQueueType(queueType string) string {
	switch queueType {
	case "deliverymq":
		return c.DeliveryTopic
	case "logmq":
		return c.LogTopic
	default:
		return ""
	}
}

func (c *AzureServiceBusConfig) getSubscriptionByQueueType(queueType string) string {
	switch queueType {
	case "deliverymq":
		return c.DeliverySubscription
	case "logmq":
		return c.LogSubscription
	default:
		return ""
	}
}

func (c *AzureServiceBusConfig) ToInfraConfig(queueType string) *mqinfra.MQInfraConfig {
	if !c.IsConfigured() {
		return nil
	}

	topic := c.getTopicByQueueType(queueType)
	subscription := c.getSubscriptionByQueueType(queueType)

	return &mqinfra.MQInfraConfig{
		AzureServiceBus: &mqinfra.AzureServiceBusInfraConfig{
			ConnectionString: c.ConnectionString,
			TenantID:         c.TenantID,
			ClientID:         c.ClientID,
			ClientSecret:     c.ClientSecret,
			SubscriptionID:   c.SubscriptionID,
			ResourceGroup:    c.ResourceGroup,
			Namespace:        c.Namespace,
			Topic:            topic,
			Subscription:     subscription,
		},
	}
}

func (c *AzureServiceBusConfig) ToQueueConfig(ctx context.Context, queueType string) (*mqs.QueueConfig, error) {
	if !c.IsConfigured() {
		return nil, nil
	}

	topic := c.getTopicByQueueType(queueType)
	subscription := c.getSubscriptionByQueueType(queueType)

	if c.ConnectionString != "" {
		return &mqs.QueueConfig{
			AzureServiceBus: &mqs.AzureServiceBusConfig{
				ConnectionString: c.ConnectionString,
				Topic:            topic,
				Subscription:     subscription,
			},
		}, nil
	}

	return &mqs.QueueConfig{
		AzureServiceBus: &mqs.AzureServiceBusConfig{
			Topic:          topic,
			Subscription:   subscription,
			TenantID:       c.TenantID,
			ClientID:       c.ClientID,
			ClientSecret:   c.ClientSecret,
			SubscriptionID: c.SubscriptionID,
			ResourceGroup:  c.ResourceGroup,
			Namespace:      c.Namespace,
		},
	}, nil
}

// getConnectionString fetches the namespace's primary connection string using ARM API.
// This method is not currently used because we've adopted direct Service Principal authentication.
//
// Permission requirements:
// - Connection string: Requires management plane access (e.g., "Contributor" role) to list namespace keys
// - Direct auth: Requires only data plane access ("Azure Service Bus Data Owner" role)
// func (c *AzureServiceBusConfig) getConnectionString(ctx context.Context) (string, error) {
// 	c.connectionStringOnce.Do(func() {
// 		cred, err := azidentity.NewClientSecretCredential(
// 			c.TenantID,
// 			c.ClientID,
// 			c.ClientSecret,
// 			nil,
// 		)
// 		if err != nil {
// 			c.connectionStringError = fmt.Errorf("failed to create credential: %w", err)
// 			return
// 		}

// 		sbClient, err := armservicebus.NewNamespacesClient(c.SubscriptionID, cred, nil)
// 		if err != nil {
// 			c.connectionStringError = fmt.Errorf("failed to create servicebus client: %w", err)
// 			return
// 		}

// 		keysResp, err := sbClient.ListKeys(ctx, c.ResourceGroup, c.Namespace, "RootManageSharedAccessKey", nil)
// 		if err != nil {
// 			c.connectionStringError = fmt.Errorf("failed to get keys: %w", err)
// 			return
// 		}

// 		if keysResp.PrimaryConnectionString == nil {
// 			c.connectionStringError = fmt.Errorf("no connection string found")
// 			return
// 		}

// 		c.connectionString = *keysResp.PrimaryConnectionString
// 	})

// 	return c.connectionString, c.connectionStringError
// }
