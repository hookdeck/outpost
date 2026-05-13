# Destination


## Supported Types

### DestinationWebhook

```go
destination := components.CreateDestinationWebhook(components.DestinationWebhook{/* values here */})
```

### DestinationAWSSQS

```go
destination := components.CreateDestinationAwsSqs(components.DestinationAWSSQS{/* values here */})
```

### DestinationRabbitMQ

```go
destination := components.CreateDestinationRabbitmq(components.DestinationRabbitMQ{/* values here */})
```

### DestinationHookdeck

```go
destination := components.CreateDestinationHookdeck(components.DestinationHookdeck{/* values here */})
```

### DestinationAWSKinesis

```go
destination := components.CreateDestinationAwsKinesis(components.DestinationAWSKinesis{/* values here */})
```

### DestinationAzureServiceBus

```go
destination := components.CreateDestinationAzureServicebus(components.DestinationAzureServiceBus{/* values here */})
```

### DestinationAwss3

```go
destination := components.CreateDestinationAwsS3(components.DestinationAwss3{/* values here */})
```

### DestinationGCPPubSub

```go
destination := components.CreateDestinationGcpPubsub(components.DestinationGCPPubSub{/* values here */})
```

### DestinationKafka

```go
destination := components.CreateDestinationKafka(components.DestinationKafka{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch destination.Type {
	case components.DestinationUnionTypeWebhook:
		// destination.DestinationWebhook is populated
	case components.DestinationUnionTypeAwsSqs:
		// destination.DestinationAWSSQS is populated
	case components.DestinationUnionTypeRabbitmq:
		// destination.DestinationRabbitMQ is populated
	case components.DestinationUnionTypeHookdeck:
		// destination.DestinationHookdeck is populated
	case components.DestinationUnionTypeAwsKinesis:
		// destination.DestinationAWSKinesis is populated
	case components.DestinationUnionTypeAzureServicebus:
		// destination.DestinationAzureServiceBus is populated
	case components.DestinationUnionTypeAwsS3:
		// destination.DestinationAwss3 is populated
	case components.DestinationUnionTypeGcpPubsub:
		// destination.DestinationGCPPubSub is populated
	case components.DestinationUnionTypeKafka:
		// destination.DestinationKafka is populated
}
```
