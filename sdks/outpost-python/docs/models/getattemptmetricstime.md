# GetAttemptMetricsTime

Time range for the metrics query.


## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          | Example                                                              |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `start`                                                              | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_check_mark:                                                   | Start of the time range (inclusive). ISO 8601 timestamp.             | 2026-03-02T00:00:00Z                                                 |
| `end`                                                                | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_check_mark:                                                   | End of the time range (exclusive). ISO 8601 timestamp.               | 2026-03-03T00:00:00Z                                                 |