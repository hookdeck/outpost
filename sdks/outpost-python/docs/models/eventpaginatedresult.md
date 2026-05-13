# EventPaginatedResult

Paginated list of events.


## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `pagination`                                                   | [Optional[models.SeekPagination]](../models/seekpagination.md) | :heavy_minus_sign:                                             | Cursor-based pagination metadata for list responses.           |
| `models`                                                       | List[[models.Event](../models/event.md)]                       | :heavy_minus_sign:                                             | Array of event objects.                                        |