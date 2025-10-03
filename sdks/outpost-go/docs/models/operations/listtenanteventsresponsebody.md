# ListTenantEventsResponseBody

A paginated list of events.


## Fields

| Field                                                       | Type                                                        | Required                                                    | Description                                                 | Example                                                     |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `Count`                                                     | *int64*                                                     | :heavy_check_mark:                                          | Total number of items across all pages                      | 42                                                          |
| `Data`                                                      | [][components.Event](../../models/components/event.md)      | :heavy_check_mark:                                          | N/A                                                         |                                                             |
| `Next`                                                      | *string*                                                    | :heavy_check_mark:                                          | Cursor for next page (empty string if no next page)         |                                                             |
| `Prev`                                                      | *string*                                                    | :heavy_check_mark:                                          | Cursor for previous page (empty string if no previous page) |                                                             |