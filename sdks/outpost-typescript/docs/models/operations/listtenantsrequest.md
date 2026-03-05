# ListTenantsRequest

## Example Usage

```typescript
import { ListTenantsRequest } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantsRequest = {};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `limit`                                                                  | *number*                                                                 | :heavy_minus_sign:                                                       | Number of tenants to return per page (1-100, default 20).                |
| `dir`                                                                    | [operations.ListTenantsDir](../../models/operations/listtenantsdir.md)   | :heavy_minus_sign:                                                       | Sort direction.                                                          |
| `next`                                                                   | *string*                                                                 | :heavy_minus_sign:                                                       | Cursor for the next page of results. Mutually exclusive with `prev`.     |
| `prev`                                                                   | *string*                                                                 | :heavy_minus_sign:                                                       | Cursor for the previous page of results. Mutually exclusive with `next`. |