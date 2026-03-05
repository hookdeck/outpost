# DestinationUpdate


## Supported Types

### DestinationUpdateWebhook

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateWebhook(components.DestinationUpdateWebhook{/* values here */})
```

### DestinationUpdateAWSSQS

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateAWSSQS(components.DestinationUpdateAWSSQS{/* values here */})
```

### DestinationUpdateRabbitMQ

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateRabbitMQ(components.DestinationUpdateRabbitMQ{/* values here */})
```

### DestinationUpdateHookdeck

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateHookdeck(components.DestinationUpdateHookdeck{/* values here */})
```

### DestinationUpdateAWSKinesis

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateAWSKinesis(components.DestinationUpdateAWSKinesis{/* values here */})
```

### DestinationUpdateAzureServiceBus

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateAzureServiceBus(components.DestinationUpdateAzureServiceBus{/* values here */})
```

### DestinationUpdateAwss3

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateAwss3(components.DestinationUpdateAwss3{/* values here */})
```

### DestinationUpdateGCPPubSub

```go
destinationUpdate := components.CreateDestinationUpdateDestinationUpdateGCPPubSub(components.DestinationUpdateGCPPubSub{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch destinationUpdate.Type {
	case components.DestinationUpdateTypeDestinationUpdateWebhook:
		// destinationUpdate.DestinationUpdateWebhook is populated
	case components.DestinationUpdateTypeDestinationUpdateAWSSQS:
		// destinationUpdate.DestinationUpdateAWSSQS is populated
	case components.DestinationUpdateTypeDestinationUpdateRabbitMQ:
		// destinationUpdate.DestinationUpdateRabbitMQ is populated
	case components.DestinationUpdateTypeDestinationUpdateHookdeck:
		// destinationUpdate.DestinationUpdateHookdeck is populated
	case components.DestinationUpdateTypeDestinationUpdateAWSKinesis:
		// destinationUpdate.DestinationUpdateAWSKinesis is populated
	case components.DestinationUpdateTypeDestinationUpdateAzureServiceBus:
		// destinationUpdate.DestinationUpdateAzureServiceBus is populated
	case components.DestinationUpdateTypeDestinationUpdateAwss3:
		// destinationUpdate.DestinationUpdateAwss3 is populated
	case components.DestinationUpdateTypeDestinationUpdateGCPPubSub:
		// destinationUpdate.DestinationUpdateGCPPubSub is populated
}
```
