# MetricsResponse

## Example Usage

```typescript
import { MetricsResponse } from "@hookdeck/outpost-sdk/models/components";

let value: MetricsResponse = {
  data: [
    {
      timeBucket: new Date("2026-03-02T14:00:00Z"),
      dimensions: {
        "destination_id": "dest_abc",
        "topic": "user.created",
      },
      metrics: {
        "count": 1423,
        "error_rate": 0.02,
      },
    },
  ],
  metadata: {
    granularity: "1h",
    queryTimeMs: 42,
    rowCount: 2,
    rowLimit: 100000,
    truncated: false,
  },
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `data`                                                                       | [components.MetricsDataPoint](../../models/components/metricsdatapoint.md)[] | :heavy_minus_sign:                                                           | Array of aggregated data points.                                             |
| `metadata`                                                                   | [components.MetricsMetadata](../../models/components/metricsmetadata.md)     | :heavy_minus_sign:                                                           | N/A                                                                          |