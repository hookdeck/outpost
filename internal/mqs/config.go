package mqs

type QueueConfig struct {
	AWSSQS          *AWSSQSConfig
	AzureServiceBus *AzureServiceBusConfig
	GCPPubSub       *GCPPubSubConfig
	RabbitMQ        *RabbitMQConfig
	InMemory        *InMemoryConfig // mainly for testing purposes

	Policy Policy
}

type Policy struct {
	VisibilityTimeout int // seconds
	RetryLimit        int
}

type AzureServiceBusConfig struct {
}

type GCPPubSubConfig struct {
}

type InMemoryConfig struct {
	Name string
}
