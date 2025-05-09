# Events
(*Events*)

## Overview

Operations related to event history and deliveries.

### Available Operations

* [List](#list) - List Events
* [Get](#get) - Get Event
* [ListDeliveries](#listdeliveries) - List Event Delivery Attempts
* [ListByDestination](#listbydestination) - List Events by Destination
* [GetByDestination](#getbydestination) - Get Event by Destination
* [Retry](#retry) - Retry Event Delivery

## List

Retrieves a list of events for the tenant, supporting cursor navigation (details TBD) and filtering.

### Example Usage

```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/go/client"
	"github.com/hookdeck/outpost/sdks/go/client/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := client.New(
        client.WithSecurity(components.Security{
            AdminAPIKey: client.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Events.List(ctx, client.String("<id>"), nil, nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Events != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                               | Type                                                                                    | Required                                                                                | Description                                                                             |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `ctx`                                                                                   | [context.Context](https://pkg.go.dev/context#Context)                                   | :heavy_check_mark:                                                                      | The context to use for the request.                                                     |
| `tenantID`                                                                              | **string*                                                                               | :heavy_minus_sign:                                                                      | The ID of the tenant. Required when using AdminApiKey authentication.                   |
| `destinationID`                                                                         | [*operations.DestinationID](../../models/operations/destinationid.md)                   | :heavy_minus_sign:                                                                      | Filter events by destination ID(s).                                                     |
| `status`                                                                                | [*operations.ListTenantEventsStatus](../../models/operations/listtenanteventsstatus.md) | :heavy_minus_sign:                                                                      | Filter events by delivery status.                                                       |
| `opts`                                                                                  | [][operations.Option](../../models/operations/option.md)                                | :heavy_minus_sign:                                                                      | The options for this request.                                                           |

### Response

**[*operations.ListTenantEventsResponse](../../models/operations/listtenanteventsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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

## Get

Retrieves details for a specific event.

### Example Usage

```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/go/client"
	"github.com/hookdeck/outpost/sdks/go/client/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := client.New(
        client.WithSecurity(components.Security{
            AdminAPIKey: client.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Events.Get(ctx, "<id>", client.String("<id>"))
    if err != nil {
        log.Fatal(err)
    }
    if res.Event != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `eventID`                                                             | *string*                                                              | :heavy_check_mark:                                                    | The ID of the event.                                                  |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.GetTenantEventResponse](../../models/operations/gettenanteventresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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

## ListDeliveries

Retrieves a list of delivery attempts for a specific event, including response details.

### Example Usage

```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/go/client"
	"github.com/hookdeck/outpost/sdks/go/client/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := client.New(
        client.WithSecurity(components.Security{
            AdminAPIKey: client.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Events.ListDeliveries(ctx, "<id>", client.String("<id>"))
    if err != nil {
        log.Fatal(err)
    }
    if res.DeliveryAttempts != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `eventID`                                                             | *string*                                                              | :heavy_check_mark:                                                    | The ID of the event.                                                  |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.ListTenantEventDeliveriesResponse](../../models/operations/listtenanteventdeliveriesresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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

## ListByDestination

Retrieves events associated with a specific destination for the tenant.

### Example Usage

```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/go/client"
	"github.com/hookdeck/outpost/sdks/go/client/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := client.New(
        client.WithSecurity(components.Security{
            AdminAPIKey: client.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Events.ListByDestination(ctx, "<id>", client.String("<id>"), nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Events != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                                                         | Type                                                                                                              | Required                                                                                                          | Description                                                                                                       |
| ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                                             | [context.Context](https://pkg.go.dev/context#Context)                                                             | :heavy_check_mark:                                                                                                | The context to use for the request.                                                                               |
| `destinationID`                                                                                                   | *string*                                                                                                          | :heavy_check_mark:                                                                                                | The ID of the destination.                                                                                        |
| `tenantID`                                                                                                        | **string*                                                                                                         | :heavy_minus_sign:                                                                                                | The ID of the tenant. Required when using AdminApiKey authentication.                                             |
| `status`                                                                                                          | [*operations.ListTenantEventsByDestinationStatus](../../models/operations/listtenanteventsbydestinationstatus.md) | :heavy_minus_sign:                                                                                                | Filter events by delivery status.                                                                                 |
| `opts`                                                                                                            | [][operations.Option](../../models/operations/option.md)                                                          | :heavy_minus_sign:                                                                                                | The options for this request.                                                                                     |

### Response

**[*operations.ListTenantEventsByDestinationResponse](../../models/operations/listtenanteventsbydestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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

## GetByDestination

Retrieves a specific event associated with a specific destination for the tenant.

### Example Usage

```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/go/client"
	"github.com/hookdeck/outpost/sdks/go/client/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := client.New(
        client.WithSecurity(components.Security{
            AdminAPIKey: client.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Events.GetByDestination(ctx, "<id>", "<id>", client.String("<id>"))
    if err != nil {
        log.Fatal(err)
    }
    if res.Event != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `destinationID`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `eventID`                                                             | *string*                                                              | :heavy_check_mark:                                                    | The ID of the event.                                                  |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.GetTenantEventByDestinationResponse](../../models/operations/gettenanteventbydestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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

## Retry

Triggers a retry for a failed event delivery.

### Example Usage

```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/go/client"
	"github.com/hookdeck/outpost/sdks/go/client/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := client.New(
        client.WithSecurity(components.Security{
            AdminAPIKey: client.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Events.Retry(ctx, "<id>", "<id>", client.String("<id>"))
    if err != nil {
        log.Fatal(err)
    }
    if res != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `destinationID`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `eventID`                                                             | *string*                                                              | :heavy_check_mark:                                                    | The ID of the event to retry.                                         |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.RetryTenantEventResponse](../../models/operations/retrytenanteventresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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