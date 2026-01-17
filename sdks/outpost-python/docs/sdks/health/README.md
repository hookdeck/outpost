# Health

## Overview

API Health Check

### Available Operations

* [check](#check) - Health Check

## check

Health check endpoint that reports the status of all workers.

Returns HTTP 200 when all workers are healthy, or HTTP 503 if any worker has failed.

The response includes:
- `status`: Overall health status ("healthy" or "failed")
- `timestamp`: When this health check was performed (ISO 8601 format)
- `workers`: Map of worker names to their individual health status

Each worker reports:
- `status`: Worker health ("healthy" or "failed")

Note: Error details are not exposed for security reasons. Check application logs for detailed error information.


### Example Usage

<!-- UsageSnippet language="python" operationID="healthCheck" method="get" path="/healthz" -->
```python
from outpost_sdk import Outpost


with Outpost() as outpost:

    res = outpost.health.check()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.HealthCheckResponse](../../models/healthcheckresponse.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.NotFoundError         | 404                          | application/json             |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |