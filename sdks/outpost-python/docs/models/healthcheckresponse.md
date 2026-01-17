# HealthCheckResponse

Service is healthy - all workers are operational.


## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          | Example                                                              |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `status`                                                             | [models.HealthCheckStatus1](../models/healthcheckstatus1.md)         | :heavy_check_mark:                                                   | N/A                                                                  | healthy                                                              |
| `timestamp`                                                          | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_check_mark:                                                   | When this health check was performed                                 | 2025-11-11T10:30:00Z                                                 |
| `workers`                                                            | Dict[str, [models.Workers](../models/workers.md)]                    | :heavy_check_mark:                                                   | N/A                                                                  |                                                                      |