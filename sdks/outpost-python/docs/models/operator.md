# Operator

Comparison operators for filtering by date-time values (RFC3339 or YYYY-MM-DD format).


## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `gte`                                                                | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_minus_sign:                                                   | Filter with value >= the specified date-time.                        |
| `lte`                                                                | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_minus_sign:                                                   | Filter with value <= the specified date-time.                        |
| `gt`                                                                 | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_minus_sign:                                                   | Filter with value > the specified date-time.                         |
| `lt`                                                                 | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_minus_sign:                                                   | Filter with value < the specified date-time.                         |