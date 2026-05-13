# Metrics

## Overview

Aggregated metrics for events and delivery attempts. Supports time bucketing, dimensional grouping, and filtering.


### Available Operations

* [get_event_metrics](#get_event_metrics) - Get Event Metrics
* [get_attempt_metrics](#get_attempt_metrics) - Get Attempt Metrics

## get_event_metrics

Returns aggregated event publish metrics. Supports time bucketing via granularity,
dimensional grouping, and filtering.

**Measures:** `count`, `rate`

**Dimensions:** `tenant_id` (admin-only), `topic`, `destination_id`

**Filters:** `tenant_id` (admin-only), `topic`, `destination_id`


### Example Usage

<!-- UsageSnippet language="python" operationID="getEventMetrics" method="get" path="/metrics/events" example="HourlyEventCount" -->
```python
from outpost_sdk import Outpost, models
from outpost_sdk.utils import parse_datetime


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.metrics.get_event_metrics(request={
        "time": {
            "start": parse_datetime("2026-03-02T00:00:00Z"),
            "end": parse_datetime("2026-03-03T00:00:00Z"),
        },
        "granularity": "1h",
        "measures": [
            models.GetEventMetricsMeasuresEnum2.COUNT,
        ],
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                               | Type                                                                    | Required                                                                | Description                                                             |
| ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- | ----------------------------------------------------------------------- |
| `request`                                                               | [models.GetEventMetricsRequest](../../models/geteventmetricsrequest.md) | :heavy_check_mark:                                                      | The request object to use for the request.                              |
| `retries`                                                               | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)        | :heavy_minus_sign:                                                      | Configuration to override the default retry behavior of the client.     |

### Response

**[models.MetricsResponse](../../models/metricsresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 403                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get_attempt_metrics

Returns aggregated delivery attempt metrics. Supports time bucketing via granularity,
dimensional grouping, and filtering.

**Measures:** `count`, `successful_count`, `failed_count`, `error_rate`,
`first_attempt_count`, `retry_count`, `manual_retry_count`, `avg_attempt_number`,
`rate`, `successful_rate`, `failed_rate`

**Dimensions:** `tenant_id` (admin-only), `destination_id`, `destination_type`, `topic`, `status`, `code`, `manual`, `attempt_number`

**Filters:** `tenant_id` (admin-only), `destination_id`, `destination_type`, `topic`, `status`, `code`, `manual`, `attempt_number`


### Example Usage

<!-- UsageSnippet language="python" operationID="getAttemptMetrics" method="get" path="/metrics/attempts" example="DailyAttemptCounts" -->
```python
from outpost_sdk import Outpost, models
from outpost_sdk.utils import parse_datetime


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.metrics.get_attempt_metrics(request={
        "time": {
            "start": parse_datetime("2026-03-02T00:00:00Z"),
            "end": parse_datetime("2026-03-03T00:00:00Z"),
        },
        "granularity": "1h",
        "measures": [
            models.GetAttemptMetricsMeasuresEnum2.COUNT,
            models.GetAttemptMetricsMeasuresEnum2.ERROR_RATE,
        ],
        "filters_destination_type": models.DestinationType.WEBHOOK,
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                   | Type                                                                        | Required                                                                    | Description                                                                 |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------- | --------------------------------------------------------------------------- |
| `request`                                                                   | [models.GetAttemptMetricsRequest](../../models/getattemptmetricsrequest.md) | :heavy_check_mark:                                                          | The request object to use for the request.                                  |
| `retries`                                                                   | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)            | :heavy_minus_sign:                                                          | Configuration to override the default retry behavior of the client.         |

### Response

**[models.MetricsResponse](../../models/metricsresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 403                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |