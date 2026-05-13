# GetAttemptMetricsTime

Time range for the metrics query.

## Example Usage

```typescript
import { GetAttemptMetricsTime } from "@hookdeck/outpost-sdk/models/operations";

let value: GetAttemptMetricsTime = {
  start: new Date("2026-03-02T00:00:00Z"),
  end: new Date("2026-03-03T00:00:00Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `start`                                                                                       | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start of the time range (inclusive). ISO 8601 timestamp.                                      | 2026-03-02T00:00:00Z                                                                          |
| `end`                                                                                         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End of the time range (exclusive). ISO 8601 timestamp.                                        | 2026-03-03T00:00:00Z                                                                          |