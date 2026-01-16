# ListTenantEventsResponse

A paginated list of events.

## Example Usage

```typescript
import { ListTenantEventsResponse } from "@hookdeck/outpost-sdk/models/operations";

let value: ListTenantEventsResponse = {
  count: 42,
  data: [],
  next: "",
  prev: "",
};
```

## Fields

| Field                                                       | Type                                                        | Required                                                    | Description                                                 | Example                                                     |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `count`                                                     | *number*                                                    | :heavy_check_mark:                                          | Total number of items across all pages                      | 42                                                          |
| `data`                                                      | [components.Event](../../models/components/event.md)[]      | :heavy_check_mark:                                          | N/A                                                         |                                                             |
| `next`                                                      | *string*                                                    | :heavy_check_mark:                                          | Cursor for next page (empty string if no next page)         |                                                             |
| `prev`                                                      | *string*                                                    | :heavy_check_mark:                                          | Cursor for previous page (empty string if no previous page) |                                                             |