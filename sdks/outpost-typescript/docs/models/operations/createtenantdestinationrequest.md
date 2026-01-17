# CreateTenantDestinationRequest

## Example Usage

```typescript
import { CreateTenantDestinationRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: CreateTenantDestinationRequest = {
  params: {
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
  },
};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `params`                                                              | *components.DestinationCreate*                                        | :heavy_check_mark:                                                    | N/A                                                                   |