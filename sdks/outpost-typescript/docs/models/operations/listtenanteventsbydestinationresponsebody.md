# ListTenantEventsByDestinationResponseBody

A paginated list of events for the destination.

## Example Usage

```typescript
import { ListTenantEventsByDestinationResponseBody } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantEventsByDestinationResponseBody = {
  count: 42,
  data: [],
  nextCursor: "",
  prevCursor: "",
};
```

## Fields

| Field                                                       | Type                                                        | Required                                                    | Description                                                 | Example                                                     |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `count`                                                     | *number*                                                    | :heavy_check_mark:                                          | Total number of items across all pages                      | 42                                                          |
| `data`                                                      | [components.Event](../../models/components/event.md)[]      | :heavy_check_mark:                                          | N/A                                                         |                                                             |
| `nextCursor`                                                | *string*                                                    | :heavy_check_mark:                                          | Cursor for next page (empty string if no next page)         |                                                             |
| `prevCursor`                                                | *string*                                                    | :heavy_check_mark:                                          | Cursor for previous page (empty string if no previous page) |                                                             |