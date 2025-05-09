# UpdateTenantDestinationResponse

Destination updated successfully or OAuth redirect needed.


## Supported Types

### `components.Destination`

```typescript
const value: components.Destination = {
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

