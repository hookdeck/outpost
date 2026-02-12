# AttemptPaginatedResult

Paginated list of attempts.


## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `models`                                                       | List[[models.Attempt](../models/attempt.md)]                   | :heavy_minus_sign:                                             | Array of attempt objects.                                      |
| `pagination`                                                   | [Optional[models.SeekPagination]](../models/seekpagination.md) | :heavy_minus_sign:                                             | Cursor-based pagination metadata for list responses.           |