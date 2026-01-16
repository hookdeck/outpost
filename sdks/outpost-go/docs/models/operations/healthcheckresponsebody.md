# HealthCheckResponseBody

Service is healthy - all workers are operational.


## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    | Example                                                                        |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `Status`                                                                       | [operations.HealthCheckStatus1](../../models/operations/healthcheckstatus1.md) | :heavy_check_mark:                                                             | N/A                                                                            | healthy                                                                        |
| `Timestamp`                                                                    | [time.Time](https://pkg.go.dev/time#Time)                                      | :heavy_check_mark:                                                             | When this health check was performed                                           | 2025-11-11T10:30:00Z                                                           |
| `Workers`                                                                      | map[string][operations.Workers](../../models/operations/workers.md)            | :heavy_check_mark:                                                             | N/A                                                                            |                                                                                |