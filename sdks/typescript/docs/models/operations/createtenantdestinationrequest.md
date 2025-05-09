# CreateTenantDestinationRequest

## Example Usage

```typescript
import { CreateTenantDestinationRequest } from "openapi/models/operations";

let value: CreateTenantDestinationRequest = {
  tenantId: "<id>",
  destinationCreate: {
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
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationCreate`                                                   | *components.DestinationCreate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |