# ListTenantDestinationsRequest

## Example Usage

```typescript
import { ListTenantDestinationsRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantDestinationsRequest = {};
```

## Fields

| Field                                                                 | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenantId`                                                            | *string*                                                              | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `type`                                                                | *operations.Type*                                                     | :heavy_minus_sign:                                                    | Filter destinations by type(s).                                       |
| `topics`                                                              | *operations.Topics*                                                   | :heavy_minus_sign:                                                    | Filter destinations by supported topic(s).                            |