# Events

## Overview

Operations related to event history.

### Available Operations

* [List](#list) - List Events (Admin)
* [Get](#get) - Get Event

## List

Retrieves a list of events across all tenants. This is an admin-only endpoint that requires the Admin API Key.

When `tenant_id` is not provided, returns events from all tenants. When `tenant_id` is provided, returns only events for that tenant.


### Example Usage

<!-- UsageSnippet language="go" operationID="adminListEvents" method="get" path="/events" example="AdminEventsListExample" -->
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

    res, err := s.Events.List(ctx, operations.AdminListEventsRequest{})
    if err != nil {
        log.Fatal(err)
    }
    if res.EventPaginatedResult != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                              | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `ctx`                                                                                  | [context.Context](https://pkg.go.dev/context#Context)                                  | :heavy_check_mark:                                                                     | The context to use for the request.                                                    |
| `request`                                                                              | [operations.AdminListEventsRequest](../../models/operations/adminlisteventsrequest.md) | :heavy_check_mark:                                                                     | The request object to use for the request.                                             |
| `opts`                                                                                 | [][operations.Option](../../models/operations/option.md)                               | :heavy_minus_sign:                                                                     | The options for this request.                                                          |

### Response

**[*operations.AdminListEventsResponse](../../models/operations/adminlisteventsresponse.md), error**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| apierrors.APIErrorResponse | 422                        | application/json           |
| apierrors.APIError         | 4XX, 5XX                   | \*/\*                      |

## Get

Retrieves details for a specific event.

When authenticated with a Tenant JWT, only events belonging to that tenant can be accessed.
When authenticated with Admin API Key, events from any tenant can be accessed.


### Example Usage

<!-- UsageSnippet language="go" operationID="getEvent" method="get" path="/events/{event_id}" example="EventExample" -->
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

    res, err := s.Events.Get(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Event != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `eventID`                                                | *string*                                                 | :heavy_check_mark:                                       | The ID of the event.                                     |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.GetEventResponse](../../models/operations/geteventresponse.md), error**

### Errors

| Error Type         | Status Code        | Content Type       |
| ------------------ | ------------------ | ------------------ |
| apierrors.APIError | 4XX, 5XX           | \*/\*              |