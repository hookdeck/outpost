# Metrics

## Overview

Aggregated metrics for events and delivery attempts. Supports time bucketing, dimensional grouping, and filtering.


### Available Operations

* [getEventMetrics](#geteventmetrics) - Get Event Metrics
* [getAttemptMetrics](#getattemptmetrics) - Get Attempt Metrics

## getEventMetrics

Returns aggregated event publish metrics. Supports time bucketing via granularity,
dimensional grouping, and filtering.

**Measures:** `count`, `rate`

**Dimensions:** `tenant_id` (admin-only), `topic`, `destination_id`

**Filters:** `tenant_id` (admin-only), `topic`, `destination_id`


### Example Usage

<!-- UsageSnippet language="typescript" operationID="getEventMetrics" method="get" path="/metrics/events" example="HourlyEventCount" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await outpost.metrics.getEventMetrics({
    time: {
      start: new Date("2026-03-02T00:00:00Z"),
      end: new Date("2026-03-03T00:00:00Z"),
    },
    granularity: "1h",
    measures: [
      "count",
    ],
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { metricsGetEventMetrics } from "@hookdeck/outpost-sdk/funcs/metricsGetEventMetrics.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const res = await metricsGetEventMetrics(outpost, {
    time: {
      start: new Date("2026-03-02T00:00:00Z"),
      end: new Date("2026-03-03T00:00:00Z"),
    },
    granularity: "1h",
    measures: [
      "count",
    ],
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("metricsGetEventMetrics failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetEventMetricsRequest](../../models/operations/geteventmetricsrequest.md)                                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.MetricsResponse](../../models/components/metricsresponse.md)\>**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 403                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## getAttemptMetrics

Returns aggregated delivery attempt metrics. Supports time bucketing via granularity,
dimensional grouping, and filtering.

**Measures:** `count`, `successful_count`, `failed_count`, `error_rate`,
`first_attempt_count`, `retry_count`, `manual_retry_count`, `avg_attempt_number`,
`rate`, `successful_rate`, `failed_rate`

**Dimensions:** `tenant_id` (admin-only), `destination_id`, `destination_type`, `topic`, `status`, `code`, `manual`, `attempt_number`

**Filters:** `tenant_id` (admin-only), `destination_id`, `destination_type`, `topic`, `status`, `code`, `manual`, `attempt_number`


### Example Usage

<!-- UsageSnippet language="typescript" operationID="getAttemptMetrics" method="get" path="/metrics/attempts" example="DailyAttemptCounts" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await outpost.metrics.getAttemptMetrics({
    time: {
      start: new Date("2026-03-02T00:00:00Z"),
      end: new Date("2026-03-03T00:00:00Z"),
    },
    granularity: "1h",
    measures: [
      "count",
      "error_rate",
    ],
    filtersDestinationType: "webhook",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { metricsGetAttemptMetrics } from "@hookdeck/outpost-sdk/funcs/metricsGetAttemptMetrics.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const res = await metricsGetAttemptMetrics(outpost, {
    time: {
      start: new Date("2026-03-02T00:00:00Z"),
      end: new Date("2026-03-03T00:00:00Z"),
    },
    granularity: "1h",
    measures: [
      "count",
      "error_rate",
    ],
    filtersDestinationType: "webhook",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("metricsGetAttemptMetrics failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetAttemptMetricsRequest](../../models/operations/getattemptmetricsrequest.md)                                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.MetricsResponse](../../models/components/metricsresponse.md)\>**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 403                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |