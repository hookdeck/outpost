# AttemptPaginatedResult

Paginated list of attempts.


## Fields

| Field                                                                   | Type                                                                    | Required                                                                | Description                                                             |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `Models`                                                                | [][components.Attempt](../../models/components/attempt.md)              | :heavy_minus_sign:                                                      | Array of attempt objects.                                               |
| `Pagination`                                                            | [*components.SeekPagination](../../models/components/seekpagination.md) | :heavy_minus_sign:                                                      | Cursor-based pagination metadata for list responses.                    |