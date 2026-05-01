# Publish

## Overview

Use the Publish endpoint to send events into Outpost. Events are matched against all destinations whose topic subscriptions and filters match the event. Requires Admin API Key.

### Available Operations

* [Event](#event) - Publish Event

## Event

Publishes an event to the specified topic, potentially routed to a specific destination. Requires Admin API Key.

### Example Usage

<!-- UsageSnippet language="go" operationID="publishEvent" method="post" path="/publish" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Publish.Event(ctx, components.PublishRequest{
        ID: outpostgo.Pointer("evt_abc123xyz789"),
        TenantID: outpostgo.Pointer("tenant_123"),
        Topic: outpostgo.Pointer("user.created"),
        EligibleForRetry: outpostgo.Pointer(true),
        Metadata: map[string]string{
            "source": "crm",
        },
        Data: map[string]any{
            "user_id": "userid",
            "status": "active",
        },
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.PublishResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                              | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `ctx`                                                                  | [context.Context](https://pkg.go.dev/context#Context)                  | :heavy_check_mark:                                                     | The context to use for the request.                                    |
| `request`                                                              | [components.PublishRequest](../../models/components/publishrequest.md) | :heavy_check_mark:                                                     | The request object to use for the request.                             |
| `opts`                                                                 | [][operations.Option](../../models/operations/option.md)               | :heavy_minus_sign:                                                     | The options for this request.                                          |

### Response

**[*operations.PublishEventResponse](../../models/operations/publisheventresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.UnauthorizedError   | 401, 403, 407                 | application/json              |
| apierrors.TimeoutError        | 408                           | application/json              |
| apierrors.RateLimitedError    | 429                           | application/json              |
| apierrors.BadRequestError     | 413, 414, 415, 422, 431       | application/json              |
| apierrors.TimeoutError        | 504                           | application/json              |
| apierrors.NotFoundError       | 501, 505                      | application/json              |
| apierrors.InternalServerError | 500, 502, 503, 506, 507, 508  | application/json              |
| apierrors.BadRequestError     | 510                           | application/json              |
| apierrors.UnauthorizedError   | 511                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |