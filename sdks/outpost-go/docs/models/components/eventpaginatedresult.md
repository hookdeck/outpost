# EventPaginatedResult

Paginated list of events.


## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `Models`                                                                | [][components.Event](../../models/components/event.md)                  | :heavy_minus_sign:                                                      | Array of event objects.                                                 |
| `Pagination`                                                            | [*components.SeekPagination](../../models/components/seekpagination.md) | :heavy_minus_sign:                                                      | Cursor-based pagination metadata for list responses.                    |