# MetricsDataPoint

## Example Usage

```typescript
import { MetricsDataPoint } from "@hookdeck/outpost-sdk/models/components";

let value: MetricsDataPoint = {
  timeBucket: new Date("2026-03-02T14:00:00Z"),
  dimensions: {
    "destination_id": "dest_abc",
    "topic": "user.created",
  },
  metrics: {
    "count": 1423,
    "error_rate": 0.02,
  },
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `timeBucket`                                                                                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Start of the time bucket. Absent when no granularity is specified.                            | 2026-03-02T14:00:00Z                                                                          |
| `dimensions`                                                                                  | Record<string, *string*>                                                                      | :heavy_minus_sign:                                                                            | Dimension values for this data point. Empty object when no dimensions are requested.          | {<br/>"destination_id": "dest_abc",<br/>"topic": "user.created"<br/>}                         |
| `metrics`                                                                                     | Record<string, *any*>                                                                         | :heavy_minus_sign:                                                                            | Requested measure values for this data point.                                                 | {<br/>"count": 1423,<br/>"error_rate": 0.02<br/>}                                             |