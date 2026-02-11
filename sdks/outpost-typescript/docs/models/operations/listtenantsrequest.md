# ListTenantsRequest

## Example Usage

```typescript
import { ListTenantsRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantsRequest = {};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `limit`                                                                                       | *number*                                                                                      | :heavy_minus_sign:                                                                            | Number of tenants to return per page (1-100, default 20).                                     |
| `orderBy`                                                                                     | [operations.ListTenantsOrderBy](../../models/operations/listtenantsorderby.md)                | :heavy_minus_sign:                                                                            | Field to sort by.                                                                             |
| `dir`                                                                                         | [operations.ListTenantsDir](../../models/operations/listtenantsdir.md)                        | :heavy_minus_sign:                                                                            | Sort direction.                                                                               |
| `createdAtGte`                                                                                | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Filter tenants created at or after this time (RFC3339 or YYYY-MM-DD format).                  |
| `createdAtLte`                                                                                | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Filter tenants created at or before this time (RFC3339 or YYYY-MM-DD format).                 |
| `next`                                                                                        | *string*                                                                                      | :heavy_minus_sign:                                                                            | Cursor for the next page of results. Mutually exclusive with `prev`.                          |
| `prev`                                                                                        | *string*                                                                                      | :heavy_minus_sign:                                                                            | Cursor for the previous page of results. Mutually exclusive with `next`.                      |