package mqs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/messaging/azservicebus"
	"gocloud.dev/pubsub"
	"gocloud.dev/pubsub/azuresb"
)

type AzureServiceBusConfig struct {
	Topic        string
	Subscription string
	DLQ          bool // Set to true to subscribe to the dead letter queue

	// Credentials
	ConnectionString string
	// or
	TenantID       string
	ClientID       string
	ClientSecret   string
	SubscriptionID string
	ResourceGroup  string
	Namespace      string
}

type AzureServiceBusQueue struct {
	base   *wrappedBaseQueue
	once   *sync.Once
	client *azservicebus.Client
	config *AzureServiceBusConfig
	topic  *pubsub.Topic
}

var _ Queue = &AzureServiceBusQueue{}

func (q *AzureServiceBusQueue) Init(ctx context.Context) (func(), error) {
	var err error
	q.once.Do(func() {
		err = q.InitClient(ctx)
	})
	if err != nil {
		return nil, err
	}

	sender, err := q.client.NewSender(q.config.Topic, nil)
	if err != nil {
		return nil, err
	}

	q.topic, err = azuresb.OpenTopic(ctx, sender, nil)
	if err != nil {
		return nil, err
	}

	return func() {
		if q.client != nil {
			q.client.Close(ctx)
		}
		if q.topic != nil {
			q.topic.Shutdown(ctx)
		}
	}, nil
}

func (q *AzureServiceBusQueue) Publish(ctx context.Context, incomingMessage IncomingMessage) error {
	return q.base.Publish(ctx, q.topic, incomingMessage, nil)
}

func (q *AzureServiceBusQueue) Subscribe(ctx context.Context, opts ...SubscribeOption) (Subscription, error) {
	var err error
	q.once.Do(func() {
		err = q.InitClient(ctx)
	})
	if err != nil {
		return nil, err
	}

	// Configure receiver options based on DLQ setting
	var receiverOptions *azservicebus.ReceiverOptions
	if q.config.DLQ {
		// Subscribe to the dead letter queue
		receiverOptions = &azservicebus.ReceiverOptions{
			SubQueue: azservicebus.SubQueueDeadLetter,
		}
	}

	receiver, err := q.client.NewReceiverForSubscription(q.config.Topic, q.config.Subscription, receiverOptions)
	if err != nil {
		return nil, err
	}

	subscription, err := azuresb.OpenSubscription(ctx, q.client, receiver, &azuresb.SubscriptionOptions{
		ListenerTimeout: 10 * time.Second, // Increased for cross-region scenarios
	})
	if err != nil {
		return nil, err
	}

	return q.base.Subscribe(ctx, subscription)
}

func (q *AzureServiceBusQueue) InitClient(ctx context.Context) error {
	// Case 1: Use connection string if provided
	if q.config.ConnectionString != "" {
		client, err := azservicebus.NewClientFromConnectionString(q.config.ConnectionString, nil)
		if err != nil {
			return fmt.Errorf("failed to create client from connection string: %w", err)
		}
		q.client = client
		return nil
	}

	// Case 2: Use service principal credentials directly
	if q.config.TenantID != "" && q.config.ClientID != "" && q.config.ClientSecret != "" && q.config.Namespace != "" {
		// Create credential
		cred, err := azidentity.NewClientSecretCredential(
			q.config.TenantID,
			q.config.ClientID,
			q.config.ClientSecret,
			nil,
		)
		if err != nil {
			return fmt.Errorf("failed to create credential: %w", err)
		}

		// Create client using credential and namespace FQDN
		namespaceEndpoint := q.config.Namespace + ".servicebus.windows.net"
		client, err := azservicebus.NewClient(namespaceEndpoint, cred, nil)
		if err != nil {
			return fmt.Errorf("failed to create client from credentials: %w", err)
		}
		q.client = client
		return nil
	}

	// Case 3: Neither connection string nor credentials provided
	return fmt.Errorf("azure service bus configuration incomplete: must provide either connection_string or (tenant_id, client_id, client_secret, namespace)")
}

func NewAzureServiceBusQueue(config *AzureServiceBusConfig) *AzureServiceBusQueue {
	var once sync.Once
	return &AzureServiceBusQueue{
		config: config,
		once:   &once,
		base:   newWrappedBaseQueue(),
	}
}
