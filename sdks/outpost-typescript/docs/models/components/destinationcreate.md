# DestinationCreate


## Supported Types

### `components.DestinationCreateWebhook`

```typescript
const value: components.DestinationCreateWebhook = {
  id: "user-provided-id",
  type: "webhook",
  topics: "*",
  config: {
    url: "https://example.com/webhooks/user",
  },
  credentials: {
    secret: "whsec_abc123",
    previousSecret: "whsec_xyz789",
    previousSecretInvalidAt: new Date("2024-01-02T00:00:00Z"),
  },
};
```

### `components.DestinationCreateAWSSQS`

```typescript
const value: components.DestinationCreateAWSSQS = {
  id: "user-provided-id",
  type: "aws_sqs",
  topics: "*",
  config: {
    endpoint: "https://sqs.us-east-1.amazonaws.com",
    queueUrl: "https://sqs.us-east-1.amazonaws.com/123456789012/my-queue",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
};
```

### `components.DestinationCreateRabbitMQ`

```typescript
const value: components.DestinationCreateRabbitMQ = {
  id: "user-provided-id",
  type: "rabbitmq",
  topics: "*",
  config: {
    serverUrl: "localhost:5672",
    exchange: "my-exchange",
    tls: "false",
  },
  credentials: {
    username: "guest",
    password: "guest",
  },
};
```

### `components.DestinationCreateHookdeck`

```typescript
const value: components.DestinationCreateHookdeck = {
  id: "user-provided-id",
  type: "hookdeck",
  topics: "*",
  credentials: {
    token: "hd_token_...",
  },
};
```

### `components.DestinationCreateAWSKinesis`

```typescript
const value: components.DestinationCreateAWSKinesis = {
  id: "user-provided-id",
  type: "aws_kinesis",
  topics: "*",
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
};
```

### `components.DestinationCreateAzureServiceBus`

```typescript
const value: components.DestinationCreateAzureServiceBus = {
  id: "user-provided-id",
  type: "azure_servicebus",
  topics: "*",
  config: {
    name: "my-queue-or-topic",
  },
  credentials: {
    connectionString:
      "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=abc123",
  },
};
```

### `components.DestinationCreateAwss3`

```typescript
const value: components.DestinationCreateAwss3 = {
  id: "user-provided-id",
  type: "aws_s3",
  topics: "*",
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
};
```

### `components.DestinationCreateGCPPubSub`

```typescript
const value: components.DestinationCreateGCPPubSub = {
  id: "user-provided-id",
  type: "gcp_pubsub",
  topics: "*",
  config: {
    projectId: "my-project-123",
    topic: "events-topic",
    endpoint: "pubsub.googleapis.com:443",
  },
  credentials: {
    serviceAccountJson:
      "{\"type\":\"service_account\",\"project_id\":\"my-project\",\"private_key_id\":\"key123\",\"private_key\":\"-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----\n\",\"client_email\":\"my-service@my-project.iam.gserviceaccount.com\"}",
  },
};
```

