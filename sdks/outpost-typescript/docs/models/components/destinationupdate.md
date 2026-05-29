# DestinationUpdate


## Supported Types

### `components.DestinationUpdateWebhook`

```typescript
const value: components.DestinationUpdateWebhook = {
  type: "webhook",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  config: {
    url: "https://example.com/webhooks/user",
    customHeaders:
      "{\"x-api-key\":\"secret123\",\"x-tenant-id\":\"customer-456\"}",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateAWSSQS`

```typescript
const value: components.DestinationUpdateAWSSQS = {
  type: "aws_sqs",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  config: {
    endpoint: "https://sqs.us-east-1.amazonaws.com",
    queueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateRabbitMQ`

```typescript
const value: components.DestinationUpdateRabbitMQ = {
  type: "rabbitmq",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateHookdeck`

```typescript
const value: components.DestinationUpdateHookdeck = {
  type: "hookdeck",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateAWSKinesis`

```typescript
const value: components.DestinationUpdateAWSKinesis = {
  type: "aws_kinesis",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateAzureServiceBus`

```typescript
const value: components.DestinationUpdateAzureServiceBus = {
  type: "azure_servicebus",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateAwss3`

```typescript
const value: components.DestinationUpdateAwss3 = {
  type: "aws_s3",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateGCPPubSub`

```typescript
const value: components.DestinationUpdateGCPPubSub = {
  type: "gcp_pubsub",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

### `components.DestinationUpdateKafka`

```typescript
const value: components.DestinationUpdateKafka = {
  type: "kafka",
  topics: "*",
  filter: {
    "data": {
      "amount": {
        "$gte": 100,
      },
      "customer": {
        "tier": "premium",
      },
    },
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
  disabledAt: null,
};
```

