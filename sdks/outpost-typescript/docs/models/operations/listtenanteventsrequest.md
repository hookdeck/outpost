# ListTenantEventsRequest

## Example Usage

```typescript
import { ListTenantEventsRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantEventsRequest = {};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `tenantId`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the tenant. Required when using AdminApiKey authentication.                         |
| `destinationId`                                                                               | *operations.DestinationId*                                                                    | :heavy_minus_sign:                                                                            | Filter events by destination ID(s).                                                           |
| `status`                                                                                      | [operations.ListTenantEventsStatus](../../models/operations/listtenanteventsstatus.md)        | :heavy_minus_sign:                                                                            | Filter events by delivery status.                                                             |
| `next`                                                                                        | *string*                                                                                      | :heavy_minus_sign:                                                                            | Cursor for next page of results                                                               |
| `prev`                                                                                        | *string*                                                                                      | :heavy_minus_sign:                                                                            | Cursor for previous page of results                                                           |
| `limit`                                                                                       | *number*                                                                                      | :heavy_minus_sign:                                                                            | Number of items per page (default 100, max 1000)                                              |
| `start`                                                                                       | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Start time filter (RFC3339 format)                                                            |
| `end`                                                                                         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | End time filter (RFC3339 format)                                                              |