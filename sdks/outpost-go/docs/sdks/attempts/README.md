# Attempts

## Overview

Attempts represent individual delivery attempts of events to destinations. The attempts API provides an attempt-centric view of event processing.

Each attempt contains:
- `id`: Unique attempt identifier
- `status`: success or failed
- `time`: Timestamp of the attempt
- `code`: HTTP status code or error code
- `attempt`: Attempt number (1 for first attempt, 2+ for retries)
- `event`: Associated event (ID or included object)
- `destination`: Destination ID

Use the `include` query parameter to include related data:
- `include=event`: Include event summary (id, topic, time, eligible_for_retry, metadata)
- `include=event.data`: Include full event with payload data
- `include=response_data`: Include response body and headers from the attempt


### Available Operations

* [List](#list) - List Attempts (Admin)
* [Get](#get) - Get Attempt
* [Retry](#retry) - Retry Event Delivery

## List

Retrieves a paginated list of attempts across all tenants. This is an admin-only endpoint that requires the Admin API Key.

When `tenant_id` is not provided, returns attempts from all tenants. When `tenant_id` is provided, returns only attempts for that tenant.


### Example Usage: AdminAttemptsListExample

<!-- UsageSnippet language="go" operationID="adminListAttempts" method="get" path="/attempts" example="AdminAttemptsListExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Attempts.List(ctx, operations.AdminListAttemptsRequest{})
    if err != nil {
        log.Fatal(err)
    }
    if res.AttemptPaginatedResult != nil {
        // handle response
    }
}
```
### Example Usage: AdminAttemptsWithIncludeExample

<!-- UsageSnippet language="go" operationID="adminListAttempts" method="get" path="/attempts" example="AdminAttemptsWithIncludeExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Attempts.List(ctx, operations.AdminListAttemptsRequest{})
    if err != nil {
        log.Fatal(err)
    }
    if res.AttemptPaginatedResult != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                                  | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `ctx`                                                                                      | [context.Context](https://pkg.go.dev/context#Context)                                      | :heavy_check_mark:                                                                         | The context to use for the request.                                                        |
| `request`                                                                                  | [operations.AdminListAttemptsRequest](../../models/operations/adminlistattemptsrequest.md) | :heavy_check_mark:                                                                         | The request object to use for the request.                                                 |
| `opts`                                                                                     | [][operations.Option](../../models/operations/option.md)                                   | :heavy_minus_sign:                                                                         | The options for this request.                                                              |

### Response

**[*operations.AdminListAttemptsResponse](../../models/operations/adminlistattemptsresponse.md), error**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| apierrors.APIErrorResponse | 422                        | application/json           |
| apierrors.APIError         | 4XX, 5XX                   | \*/\*                      |

## Get

Retrieves details for a specific attempt.

When authenticated with a Tenant JWT, only attempts belonging to that tenant can be accessed.
When authenticated with Admin API Key, attempts from any tenant can be accessed.


### Example Usage: AttemptExample

<!-- UsageSnippet language="go" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Attempts.Get(ctx, "<id>", nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Attempt != nil {
        // handle response
    }
}
```
### Example Usage: AttemptWithIncludeExample

<!-- UsageSnippet language="go" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptWithIncludeExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Attempts.Get(ctx, "<id>", nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Attempt != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                                                                                                                                                                                                                          | Type                                                                                                                                                                                                                                                                               | Required                                                                                                                                                                                                                                                                           | Description                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                                                                                                                                                                                                              | [context.Context](https://pkg.go.dev/context#Context)                                                                                                                                                                                                                              | :heavy_check_mark:                                                                                                                                                                                                                                                                 | The context to use for the request.                                                                                                                                                                                                                                                |
| `attemptID`                                                                                                                                                                                                                                                                        | *string*                                                                                                                                                                                                                                                                           | :heavy_check_mark:                                                                                                                                                                                                                                                                 | The ID of the attempt.                                                                                                                                                                                                                                                             |
| `include`                                                                                                                                                                                                                                                                          | [*operations.GetAttemptInclude](../../models/operations/getattemptinclude.md)                                                                                                                                                                                                      | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Fields to include in the response. Can be specified multiple times or comma-separated.<br/>- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/> |
| `opts`                                                                                                                                                                                                                                                                             | [][operations.Option](../../models/operations/option.md)                                                                                                                                                                                                                           | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | The options for this request.                                                                                                                                                                                                                                                      |

### Response

**[*operations.GetAttemptResponse](../../models/operations/getattemptresponse.md), error**

### Errors

| Error Type         | Status Code        | Content Type       |
| ------------------ | ------------------ | ------------------ |
| apierrors.APIError | 4XX, 5XX           | \*/\*              |

## Retry

Triggers a retry for delivering an event to a destination. The event must exist and the destination must be enabled and match the event's topic.

When authenticated with a Tenant JWT, only events belonging to that tenant can be retried.
When authenticated with Admin API Key, events from any tenant can be retried.


### Example Usage

<!-- UsageSnippet language="go" operationID="retryEvent" method="post" path="/retry" example="RetryAccepted" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Attempts.Retry(ctx, components.RetryRequest{
        EventID: "evt_123",
        DestinationID: "des_456",
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.SuccessResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                          | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `ctx`                                                              | [context.Context](https://pkg.go.dev/context#Context)              | :heavy_check_mark:                                                 | The context to use for the request.                                |
| `request`                                                          | [components.RetryRequest](../../models/components/retryrequest.md) | :heavy_check_mark:                                                 | The request object to use for the request.                         |
| `opts`                                                             | [][operations.Option](../../models/operations/option.md)           | :heavy_minus_sign:                                                 | The options for this request.                                      |

### Response

**[*operations.RetryEventResponse](../../models/operations/retryeventresponse.md), error**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| apierrors.APIErrorResponse | 422                        | application/json           |
| apierrors.APIError         | 4XX, 5XX                   | \*/\*                      |