# ListTenantDestinationsRequest

## Example Usage

```typescript
import { ListTenantDestinationsRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantDestinationsRequest = {
  tenantId: "<id>",
  type: "webhook",
};
```

## Fields

| Field                                                                                                                                        | Type                                                                                                                                         | Required                                                                                                                                     | Description                                                                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `tenantId`                                                                                                                                   | *string*                                                                                                                                     | :heavy_check_mark:                                                                                                                           | The ID of the tenant. Required when using AdminApiKey authentication.                                                                        |
| `type`                                                                                                                                       | *operations.ListTenantDestinationsType*                                                                                                      | :heavy_minus_sign:                                                                                                                           | Filter destinations by type(s). Use bracket notation for multiple values (e.g., `type[0]=webhook&type[1]=aws_sqs`).                          |
| `topics`                                                                                                                                     | *operations.Topics*                                                                                                                          | :heavy_minus_sign:                                                                                                                           | Filter destinations by supported topic(s). Use bracket notation for multiple values (e.g., `topics[0]=user.created&topics[1]=user.deleted`). |