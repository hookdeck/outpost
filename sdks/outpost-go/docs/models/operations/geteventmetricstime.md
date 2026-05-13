# GetEventMetricsTime

Time range for the metrics query.


## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              | Example                                                  |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `Start`                                                  | [time.Time](https://pkg.go.dev/time#Time)                | :heavy_check_mark:                                       | Start of the time range (inclusive). ISO 8601 timestamp. | 2026-03-02T00:00:00Z                                     |
| `End`                                                    | [time.Time](https://pkg.go.dev/time#Time)                | :heavy_check_mark:                                       | End of the time range (exclusive). ISO 8601 timestamp.   | 2026-03-03T00:00:00Z                                     |