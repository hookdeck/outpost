# Operator

Comparison operators for filtering by date-time values (RFC3339 or YYYY-MM-DD format).


## Fields

| Field                                         | Type                                          | Required                                      | Description                                   |
| --------------------------------------------- | --------------------------------------------- | --------------------------------------------- | --------------------------------------------- |
| `Gte`                                         | [*time.Time](https://pkg.go.dev/time#Time)    | :heavy_minus_sign:                            | Filter with value >= the specified date-time. |
| `Lte`                                         | [*time.Time](https://pkg.go.dev/time#Time)    | :heavy_minus_sign:                            | Filter with value <= the specified date-time. |
| `Gt`                                          | [*time.Time](https://pkg.go.dev/time#Time)    | :heavy_minus_sign:                            | Filter with value > the specified date-time.  |
| `Lt`                                          | [*time.Time](https://pkg.go.dev/time#Time)    | :heavy_minus_sign:                            | Filter with value < the specified date-time.  |