# HealthCheckResponse

Service is healthy - all workers are operational.

## Example Usage

```typescript
import { HealthCheckResponse } from "@hookdeck/outpost-sdk/models/operations";

let value: HealthCheckResponse = {
  status: "healthy",
  timestamp: new Date("2025-11-11T10:30:00Z"),
  workers: {},
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `status`                                                                                      | [operations.HealthCheckStatus1](../../models/operations/healthcheckstatus1.md)                | :heavy_check_mark:                                                                            | N/A                                                                                           | healthy                                                                                       |
| `timestamp`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When this health check was performed                                                          | 2025-11-11T10:30:00Z                                                                          |
| `workers`                                                                                     | Record<string, [operations.Workers](../../models/operations/workers.md)>                      | :heavy_check_mark:                                                                            | N/A                                                                                           |                                                                                               |