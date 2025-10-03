# Destination


## Supported Types

### `components.DestinationWebhook`

```typescript
const value: components.DestinationWebhook = {
  id: "des_webhook_123",
  type: "webhook",
  topics: [
    "user.created",
    "order.shipped",
  ],
  disabledAt: null,
  createdAt: new Date("2024-02-15T10:00:00Z"),
  config: {
    url: "https://my-service.com/webhook/handler",
  },
  credentials: {
    secret: "whsec_abc123def456",
    previousSecret: "whsec_prev789xyz012",
    previousSecretInvalidAt: new Date("2024-02-16T10:00:00Z"),
  },
};
```

### `components.DestinationAWSSQS`

```typescript
const value: components.DestinationAWSSQS = {
  id: "des_sqs_456",
  type: "aws_sqs",
  topics: [
    "*",
  ],
  disabledAt: new Date("2024-03-01T12:00:00Z"),
  createdAt: new Date("2024-02-20T11:30:00Z"),
  config: {
    endpoint: "https://sqs.us-west-2.amazonaws.com",
    queueUrl: "https://sqs.us-west-2.amazonaws.com/123456789012/my-app-queue",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  },
};
```

### `components.DestinationRabbitMQ`

```typescript
const value: components.DestinationRabbitMQ = {
  id: "des_rmq_789",
  type: "rabbitmq",
  topics: [
    "inventory.updated",
  ],
  disabledAt: null,
  createdAt: new Date("2024-01-10T09:00:00Z"),
  config: {
    serverUrl: "amqp.cloudamqp.com:5671",
    exchange: "events-exchange",
    tls: "true",
  },
  credentials: {
    username: "app_user",
    password: "secure_password_123",
  },
};
```

### `components.DestinationHookdeck`

```typescript
const value: components.DestinationHookdeck = {
  id: "des_hkd_abc",
  type: "hookdeck",
  topics: [
    "*",
  ],
  disabledAt: null,
  createdAt: new Date("2024-04-01T10:00:00Z"),
  config: {},
  credentials: {
    token: "hd_token_...",
  },
};
```

### `components.DestinationAWSKinesis`

```typescript
const value: components.DestinationAWSKinesis = {
  id: "des_kns_xyz",
  type: "aws_kinesis",
  topics: [
    "user.created",
    "user.updated",
  ],
  disabledAt: null,
  createdAt: new Date("2024-03-10T15:30:00Z"),
  config: {
    streamName: "production-events",
    region: "eu-west-1",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  },
};
```

### `components.DestinationAzureServiceBus`

```typescript
const value: components.DestinationAzureServiceBus = {
  id: "des_azuresb_123",
  type: "azure_servicebus",
  topics: [
    "*",
  ],
  disabledAt: null,
  createdAt: new Date("2024-05-01T10:00:00Z"),
  config: {
    name: "my-queue-or-topic",
  },
  credentials: {
    connectionString:
      "Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=abc123",
  },
};
```

### `components.DestinationAwss3`

```typescript
const value: components.DestinationAwss3 = {
  id: "des_s3_789",
  type: "aws_s3",
  topics: [
    "*",
  ],
  disabledAt: null,
  createdAt: new Date("2024-03-20T12:00:00Z"),
  config: {
    bucket: "my-bucket",
    region: "us-east-1",
  },
  credentials: {
    key: "AKIAIOSFODNN7EXAMPLE",
    secret: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  },
};
```

