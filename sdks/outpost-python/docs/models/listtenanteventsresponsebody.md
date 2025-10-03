# ListTenantEventsResponseBody

A paginated list of events.


## Fields

| Field                                                       | Type                                                        | Required                                                    | Description                                                 | Example                                                     |
| ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- | ----------------------------------------------------------- |
| `count`                                                     | *int*                                                       | :heavy_check_mark:                                          | Total number of items across all pages                      | 42                                                          |
| `data`                                                      | List[[models.Event](../models/event.md)]                    | :heavy_check_mark:                                          | N/A                                                         |                                                             |
| `next_cursor`                                               | *str*                                                       | :heavy_check_mark:                                          | Cursor for next page (empty string if no next page)         |                                                             |
| `prev_cursor`                                               | *str*                                                       | :heavy_check_mark:                                          | Cursor for previous page (empty string if no previous page) |                                                             |