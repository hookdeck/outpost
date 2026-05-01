# DestinationCreate


## Supported Types

### DestinationCreateWebhook

```go
destinationCreate := components.CreateDestinationCreateWebhook(components.DestinationCreateWebhook{/* values here */})
```

### DestinationCreateAWSSQS

```go
destinationCreate := components.CreateDestinationCreateAwsSqs(components.DestinationCreateAWSSQS{/* values here */})
```

### DestinationCreateRabbitMQ

```go
destinationCreate := components.CreateDestinationCreateRabbitmq(components.DestinationCreateRabbitMQ{/* values here */})
```

### DestinationCreateHookdeck

```go
destinationCreate := components.CreateDestinationCreateHookdeck(components.DestinationCreateHookdeck{/* values here */})
```

### DestinationCreateAWSKinesis

```go
destinationCreate := components.CreateDestinationCreateAwsKinesis(components.DestinationCreateAWSKinesis{/* values here */})
```

### DestinationCreateAzureServiceBus

```go
destinationCreate := components.CreateDestinationCreateAzureServicebus(components.DestinationCreateAzureServiceBus{/* values here */})
```

### DestinationCreateAwss3

```go
destinationCreate := components.CreateDestinationCreateAwsS3(components.DestinationCreateAwss3{/* values here */})
```

### DestinationCreateGCPPubSub

```go
destinationCreate := components.CreateDestinationCreateGcpPubsub(components.DestinationCreateGCPPubSub{/* values here */})
```

### DestinationCreateKafka

```go
destinationCreate := components.CreateDestinationCreateKafka(components.DestinationCreateKafka{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch destinationCreate.Type {
	case components.DestinationCreateTypeWebhook:
		// destinationCreate.DestinationCreateWebhook is populated
	case components.DestinationCreateTypeAwsSqs:
		// destinationCreate.DestinationCreateAWSSQS is populated
	case components.DestinationCreateTypeRabbitmq:
		// destinationCreate.DestinationCreateRabbitMQ is populated
	case components.DestinationCreateTypeHookdeck:
		// destinationCreate.DestinationCreateHookdeck is populated
	case components.DestinationCreateTypeAwsKinesis:
		// destinationCreate.DestinationCreateAWSKinesis is populated
	case components.DestinationCreateTypeAzureServicebus:
		// destinationCreate.DestinationCreateAzureServiceBus is populated
	case components.DestinationCreateTypeAwsS3:
		// destinationCreate.DestinationCreateAwss3 is populated
	case components.DestinationCreateTypeGcpPubsub:
		// destinationCreate.DestinationCreateGCPPubSub is populated
	case components.DestinationCreateTypeKafka:
		// destinationCreate.DestinationCreateKafka is populated
}
```
