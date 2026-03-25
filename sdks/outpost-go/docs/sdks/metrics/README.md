# Metrics

## Overview

Aggregated metrics for events and delivery attempts. Supports time bucketing, dimensional grouping, and filtering.


### Available Operations

* [GetEventMetrics](#geteventmetrics) - Get Event Metrics
* [GetAttemptMetrics](#getattemptmetrics) - Get Attempt Metrics

## GetEventMetrics

Returns aggregated event publish metrics. Supports time bucketing via granularity,
dimensional grouping, and filtering.

**Measures:** `count`, `rate`

**Dimensions:** `tenant_id` (admin-only), `topic`, `destination_id`

**Filters:** `tenant_id` (admin-only), `topic`, `destination_id`


### Example Usage

<!-- UsageSnippet language="go" operationID="getEventMetrics" method="get" path="/metrics/events" example="HourlyEventCount" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/types"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Metrics.GetEventMetrics(ctx, operations.GetEventMetricsRequest{
        TimeStart: types.MustTimeFromString("2026-03-02T00:00:00Z"),
        TimeEnd: types.MustTimeFromString("2026-03-03T00:00:00Z"),
        Granularity: outpostgo.Pointer("1h"),
        Measures: operations.CreateGetEventMetricsMeasuresUnionArrayOfGetEventMetricsMeasuresEnum2(
            []operations.GetEventMetricsMeasuresEnum2{
                operations.GetEventMetricsMeasuresEnum2Count,
            },
        ),
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.MetricsResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                              | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `ctx`                                                                                  | [context.Context](https://pkg.go.dev/context#Context)                                  | :heavy_check_mark:                                                                     | The context to use for the request.                                                    |
| `request`                                                                              | [operations.GetEventMetricsRequest](../../models/operations/geteventmetricsrequest.md) | :heavy_check_mark:                                                                     | The request object to use for the request.                                             |
| `opts`                                                                                 | [][operations.Option](../../models/operations/option.md)                               | :heavy_minus_sign:                                                                     | The options for this request.                                                          |

### Response

**[*operations.GetEventMetricsResponse](../../models/operations/geteventmetricsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.BadRequestError     | 400                           | application/json              |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.APIErrorResponse    | 403                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## GetAttemptMetrics

Returns aggregated delivery attempt metrics. Supports time bucketing via granularity,
dimensional grouping, and filtering.

**Measures:** `count`, `successful_count`, `failed_count`, `error_rate`,
`first_attempt_count`, `retry_count`, `manual_retry_count`, `avg_attempt_number`,
`rate`, `successful_rate`, `failed_rate`

**Dimensions:** `tenant_id` (admin-only), `destination_id`, `topic`, `status`, `code`, `manual`, `attempt_number`

**Filters:** `tenant_id` (admin-only), `destination_id`, `topic`, `status`, `code`, `manual`, `attempt_number`


### Example Usage

<!-- UsageSnippet language="go" operationID="getAttemptMetrics" method="get" path="/metrics/attempts" example="DailyAttemptCounts" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/types"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Metrics.GetAttemptMetrics(ctx, operations.GetAttemptMetricsRequest{
        TimeStart: types.MustTimeFromString("2026-03-02T00:00:00Z"),
        TimeEnd: types.MustTimeFromString("2026-03-03T00:00:00Z"),
        Granularity: outpostgo.Pointer("1h"),
        Measures: operations.CreateGetAttemptMetricsMeasuresUnionArrayOfGetAttemptMetricsMeasuresEnum2(
            []operations.GetAttemptMetricsMeasuresEnum2{
                operations.GetAttemptMetricsMeasuresEnum2Count,
                operations.GetAttemptMetricsMeasuresEnum2ErrorRate,
            },
        ),
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.MetricsResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                                  | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `ctx`                                                                                      | [context.Context](https://pkg.go.dev/context#Context)                                      | :heavy_check_mark:                                                                         | The context to use for the request.                                                        |
| `request`                                                                                  | [operations.GetAttemptMetricsRequest](../../models/operations/getattemptmetricsrequest.md) | :heavy_check_mark:                                                                         | The request object to use for the request.                                                 |
| `opts`                                                                                     | [][operations.Option](../../models/operations/option.md)                                   | :heavy_minus_sign:                                                                         | The options for this request.                                                              |

### Response

**[*operations.GetAttemptMetricsResponse](../../models/operations/getattemptmetricsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.BadRequestError     | 400                           | application/json              |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.APIErrorResponse    | 403                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |