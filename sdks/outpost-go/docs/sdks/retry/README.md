# Retry

## Overview

Triggers a retry for delivering an event to a destination. The event must exist and the destination must be enabled and match the event's topic.

### Available Operations

* [Retry](#retry) - Retry Event Delivery

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
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Retry.Retry(ctx, components.RetryRequest{
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

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |