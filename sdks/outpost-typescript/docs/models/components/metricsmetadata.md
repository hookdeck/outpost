# MetricsMetadata

## Example Usage

```typescript
import { MetricsMetadata } from "@hookdeck/outpost-sdk/models/components";

let value: MetricsMetadata = {
  granularity: "1h",
  queryTimeMs: 42,
  rowCount: 2,
  rowLimit: 100000,
  truncated: false,
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              | Example                                                                  |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `granularity`                                                            | *string*                                                                 | :heavy_minus_sign:                                                       | The granularity used for time bucketing. Absent when none was specified. | 1h                                                                       |
| `queryTimeMs`                                                            | *number*                                                                 | :heavy_minus_sign:                                                       | Query execution time in milliseconds.                                    | 42                                                                       |
| `rowCount`                                                               | *number*                                                                 | :heavy_minus_sign:                                                       | Number of data points returned.                                          | 2                                                                        |
| `rowLimit`                                                               | *number*                                                                 | :heavy_minus_sign:                                                       | Maximum number of rows the query will return.                            | 100000                                                                   |
| `truncated`                                                              | *boolean*                                                                | :heavy_minus_sign:                                                       | Whether the results were truncated due to hitting the row limit.         | false                                                                    |