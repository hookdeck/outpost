# DestinationUpdate


## Supported Types

### `components.DestinationUpdateWebhook`

```typescript
const value: components.DestinationUpdateWebhook = {
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
};
```

### `components.DestinationUpdateAWSSQS`

```typescript
const value: components.DestinationUpdateAWSSQS = {
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
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
};
```

### `components.DestinationUpdateRabbitMQ`

```typescript
const value: components.DestinationUpdateRabbitMQ = {
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
    serverUrl: "localhost:5672",
    exchange: "my-exchange",
    tls: "false",
  },
  credentials: {
    username: "guest",
    password: "guest",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
};
```

### `components.DestinationUpdateHookdeck`

```typescript
const value: components.DestinationUpdateHookdeck = {
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
  credentials: {
    token: "hd_token_...",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
};
```

### `components.DestinationUpdateAWSKinesis`

```typescript
const value: components.DestinationUpdateAWSKinesis = {
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
    streamName: "my-data-stream",
    region: "us-east-1",
    endpoint: "https://kinesis.us-east-1.amazonaws.com",
    partitionKeyTemplate: "data.\"user_id\"",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
};
```

### `components.DestinationUpdateAzureServiceBus`

```typescript
const value: components.DestinationUpdateAzureServiceBus = {
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
    name: "my-queue-or-topic",
  },
  credentials: {
    connectionString:
      "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=abc123",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
};
```

### `components.DestinationUpdateAwss3`

```typescript
const value: components.DestinationUpdateAwss3 = {
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
    bucket: "my-bucket",
    region: "us-east-1",
    keyTemplate:
      "join('/', [time.year, time.month, time.day, metadata.\"event-id\", '.json'])",
    storageClass: "STANDARD",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
};
```

### `components.DestinationUpdateGCPPubSub`

```typescript
const value: components.DestinationUpdateGCPPubSub = {
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
    projectId: "my-project-123",
    topic: "events-topic",
    endpoint: "pubsub.googleapis.com:443",
  },
  credentials: {
    serviceAccountJson:
      "{\"type\":\"service_account\",\"project_id\":\"my-project\",\"private_key_id\":\"key123\",\"private_key\":\"-----BEGIN PRIVATE KEY-----\\n...\\n-----END PRIVATE KEY-----\\n\",\"client_email\":\"my-service@my-project.iam.gserviceaccount.com\"}",
  },
  deliveryMetadata: {
    "app-id": "my-app",
    "region": "us-east-1",
  },
  metadata: {
    "internal-id": "123",
    "team": "platform",
  },
};
```

