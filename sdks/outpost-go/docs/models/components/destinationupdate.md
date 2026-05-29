# DestinationUpdate


## Supported Types

### DestinationUpdateWebhook

```go
destinationUpdate := components.CreateDestinationUpdateWebhook(components.DestinationUpdateWebhook{/* values here */})
```

### DestinationUpdateAWSSQS

```go
destinationUpdate := components.CreateDestinationUpdateAwsSqs(components.DestinationUpdateAWSSQS{/* values here */})
```

### DestinationUpdateRabbitMQ

```go
destinationUpdate := components.CreateDestinationUpdateRabbitmq(components.DestinationUpdateRabbitMQ{/* values here */})
```

### DestinationUpdateHookdeck

```go
destinationUpdate := components.CreateDestinationUpdateHookdeck(components.DestinationUpdateHookdeck{/* values here */})
```

### DestinationUpdateAWSKinesis

```go
destinationUpdate := components.CreateDestinationUpdateAwsKinesis(components.DestinationUpdateAWSKinesis{/* values here */})
```

### DestinationUpdateAzureServiceBus

```go
destinationUpdate := components.CreateDestinationUpdateAzureServicebus(components.DestinationUpdateAzureServiceBus{/* values here */})
```

### DestinationUpdateAwss3

```go
destinationUpdate := components.CreateDestinationUpdateAwsS3(components.DestinationUpdateAwss3{/* values here */})
```

### DestinationUpdateGCPPubSub

```go
destinationUpdate := components.CreateDestinationUpdateGcpPubsub(components.DestinationUpdateGCPPubSub{/* values here */})
```

### DestinationUpdateKafka

```go
destinationUpdate := components.CreateDestinationUpdateKafka(components.DestinationUpdateKafka{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch destinationUpdate.Type {
	case components.DestinationUpdateTypeWebhook:
		// destinationUpdate.DestinationUpdateWebhook is populated
	case components.DestinationUpdateTypeAwsSqs:
		// destinationUpdate.DestinationUpdateAWSSQS is populated
	case components.DestinationUpdateTypeRabbitmq:
		// destinationUpdate.DestinationUpdateRabbitMQ is populated
	case components.DestinationUpdateTypeHookdeck:
		// destinationUpdate.DestinationUpdateHookdeck is populated
	case components.DestinationUpdateTypeAwsKinesis:
		// destinationUpdate.DestinationUpdateAWSKinesis is populated
	case components.DestinationUpdateTypeAzureServicebus:
		// destinationUpdate.DestinationUpdateAzureServiceBus is populated
	case components.DestinationUpdateTypeAwsS3:
		// destinationUpdate.DestinationUpdateAwss3 is populated
	case components.DestinationUpdateTypeGcpPubsub:
		// destinationUpdate.DestinationUpdateGCPPubSub is populated
	case components.DestinationUpdateTypeKafka:
		// destinationUpdate.DestinationUpdateKafka is populated
}
```
