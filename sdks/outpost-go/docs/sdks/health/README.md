# Health

## Overview

This endpoint is only available for **self-hosted** Outpost deployments. Managed Outpost health is monitored by Hookdeck.


### Available Operations

* [Check](#check) - Health Check

## Check

Health check endpoint that reports the status of all workers.

> This endpoint is only available for **self-hosted** Outpost deployments. Managed Outpost health is monitored by Hookdeck.

Worker status is one of:
- `healthy`: running.
- `degraded`: failing and being restarted, within its restart budget. The service keeps serving. Only reported for workers listed in `SUPERVISOR_RESTART_WORKERS`.
- `failed`: failed, or failing past its restart budget. With restarts enabled, Outpost keeps restarting it, and it returns to `healthy` once it recovers.

Returns HTTP 200 when no worker has failed (the overall status is `degraded` if any worker is degraded), or HTTP 503 if any worker has failed.

Note: Error details are not exposed for security reasons. Check application logs for detailed error information.


### Example Usage

<!-- UsageSnippet language="go" operationID="healthCheck" method="get" path="/healthz" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New()

    res, err := s.Health.Check(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.Object != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.HealthCheckResponse](../../models/operations/healthcheckresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.UnauthorizedError   | 401, 403, 407                 | application/json              |
| apierrors.TimeoutError        | 408                           | application/json              |
| apierrors.RateLimitedError    | 429                           | application/json              |
| apierrors.BadRequestError     | 400, 413, 414, 415, 422, 431  | application/json              |
| apierrors.TimeoutError        | 504                           | application/json              |
| apierrors.NotFoundError       | 501, 505                      | application/json              |
| apierrors.InternalServerError | 500, 502, 503, 506, 507, 508  | application/json              |
| apierrors.BadRequestError     | 510                           | application/json              |
| apierrors.UnauthorizedError   | 511                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |