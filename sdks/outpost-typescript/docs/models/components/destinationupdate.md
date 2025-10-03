# DestinationUpdate


## Supported Types

### `components.DestinationUpdateWebhook`

```typescript
const value: components.DestinationUpdateWebhook = {
  topics: "*",
  config: {
    url: "https://example.com/webhooks/user",
  },
};
```

### `components.DestinationUpdateAWSSQS`

```typescript
const value: components.DestinationUpdateAWSSQS = {
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

### `components.DestinationUpdateRabbitMQ`

```typescript
const value: components.DestinationUpdateRabbitMQ = {
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

### `components.DestinationUpdateHookdeck`

```typescript
const value: components.DestinationUpdateHookdeck = {
  topics: "*",
  credentials: {
    token: "hd_token_...",
  },
};
```

### `components.DestinationUpdateAWSKinesis`

```typescript
const value: components.DestinationUpdateAWSKinesis = {
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

### `components.DestinationUpdateAwss3`

```typescript
const value: components.DestinationUpdateAwss3 = {
  topics: "*",
  config: {
    bucket: "my-bucket",
    region: "us-east-1",
    keyTemplate:
      "join('/', [time.year, time.month, time.day, metadata.`\"event-id\"`, '.json'])",
    storageClass: "STANDARD",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
    session: "AQoDYXdzEPT//////////wEXAMPLE...",
  },
};
```

