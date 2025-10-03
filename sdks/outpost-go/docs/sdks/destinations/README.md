# Destinations
(*Destinations*)

## Overview

Destinations are the endpoints where events are sent. Each destination is associated with a tenant and can be configured to receive specific event topics.

```json
{
  "id": "des_12345", // Control plane generated ID or user provided ID
  "type": "webhooks", // Type of the destination
  "topics": ["user.created", "user.updated"], // Topics of events this destination is eligible for
  "config": {
    // Destination type specific configuration. Schema of depends on type
    "url": "https://example.com/webhooks/user"
  },
  "credentials": {
    // Destination type specific credentials. AES encrypted. Schema depends on type
    "secret": "some***********"
  },
  "disabled_at": null, // null or ISO date if disabled
  "created_at": "2024-01-01T00:00:00Z" // Date the destination was created
}
```

The `topics` array can contain either a list of topics or a wildcard `*` implying that all topics are supported. If you do not wish to implement topics for your application, you set all destination topics to `*`.

By default all destination `credentials` are obfuscated and the values cannot be read. This does not apply to the `webhook` type destination secret and each destination can expose their own obfuscation logic.


### Available Operations

* [List](#list) - List Destinations
* [Create](#create) - Create Destination
* [Get](#get) - Get Destination
* [Update](#update) - Update Destination
* [Delete](#delete) - Delete Destination
* [Enable](#enable) - Enable Destination
* [Disable](#disable) - Disable Destination

## List

Return a list of the destinations for the tenant. The endpoint is not paged.

### Example Usage

<!-- UsageSnippet language="go" operationID="listTenantDestinations" method="get" path="/{tenant_id}/destinations" -->
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
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Destinations.List(ctx, nil, nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Destinations != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `type_`                                                               | [*operations.Type](../../models/operations/type.md)                   | :heavy_minus_sign:                                                    | Filter destinations by type(s).                                       |
| `topics`                                                              | [*operations.Topics](../../models/operations/topics.md)               | :heavy_minus_sign:                                                    | Filter destinations by supported topic(s).                            |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.ListTenantDestinationsResponse](../../models/operations/listtenantdestinationsresponse.md), error**

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

## Create

Creates a new destination for the tenant. The request body structure depends on the `type`.

### Example Usage

<!-- UsageSnippet language="go" operationID="createTenantDestination" method="post" path="/{tenant_id}/destinations" -->
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
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Destinations.Create(ctx, components.CreateDestinationCreateRabbitmq(
        components.DestinationCreateRabbitMQ{
            ID: outpostgo.String("user-provided-id"),
            Type: components.DestinationCreateRabbitMQTypeRabbitmq,
            Topics: components.CreateTopicsTopicsEnum(
                components.TopicsEnumWildcard,
            ),
            Config: components.RabbitMQConfig{
                ServerURL: "localhost:5672",
                Exchange: "my-exchange",
                TLS: components.TLSFalse.ToPointer(),
            },
            Credentials: components.RabbitMQCredentials{
                Username: "guest",
                Password: "guest",
            },
        },
    ))
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                    | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `ctx`                                                                        | [context.Context](https://pkg.go.dev/context#Context)                        | :heavy_check_mark:                                                           | The context to use for the request.                                          |
| `destinationCreate`                                                          | [components.DestinationCreate](../../models/components/destinationcreate.md) | :heavy_check_mark:                                                           | N/A                                                                          |
| `tenantID`                                                                   | **string*                                                                    | :heavy_minus_sign:                                                           | The ID of the tenant. Required when using AdminApiKey authentication.        |
| `opts`                                                                       | [][operations.Option](../../models/operations/option.md)                     | :heavy_minus_sign:                                                           | The options for this request.                                                |

### Response

**[*operations.CreateTenantDestinationResponse](../../models/operations/createtenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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

## Get

Retrieves details for a specific destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="getTenantDestination" method="get" path="/{tenant_id}/destinations/{destination_id}" -->
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
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Destinations.Get(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `destinationID`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.GetTenantDestinationResponse](../../models/operations/gettenantdestinationresponse.md), error**

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

## Update

Updates the configuration of an existing destination. The request body structure depends on the destination's `type`. Type itself cannot be updated. May return an OAuth redirect URL for certain types.

### Example Usage

<!-- UsageSnippet language="go" operationID="updateTenantDestination" method="patch" path="/{tenant_id}/destinations/{destination_id}" -->
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
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Destinations.Update(ctx, "<id>", components.CreateDestinationUpdateDestinationUpdateRabbitMQ(
        components.DestinationUpdateRabbitMQ{
            Topics: outpostgo.Pointer(components.CreateTopicsTopicsEnum(
                components.TopicsEnumWildcard,
            )),
            Config: &components.RabbitMQConfig{
                ServerURL: "localhost:5672",
                Exchange: "my-exchange",
                TLS: components.TLSFalse.ToPointer(),
            },
            Credentials: &components.RabbitMQCredentials{
                Username: "guest",
                Password: "guest",
            },
        },
    ))
    if err != nil {
        log.Fatal(err)
    }
    if res.OneOf != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                    | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `ctx`                                                                        | [context.Context](https://pkg.go.dev/context#Context)                        | :heavy_check_mark:                                                           | The context to use for the request.                                          |
| `destinationID`                                                              | *string*                                                                     | :heavy_check_mark:                                                           | The ID of the destination.                                                   |
| `destinationUpdate`                                                          | [components.DestinationUpdate](../../models/components/destinationupdate.md) | :heavy_check_mark:                                                           | N/A                                                                          |
| `tenantID`                                                                   | **string*                                                                    | :heavy_minus_sign:                                                           | The ID of the tenant. Required when using AdminApiKey authentication.        |
| `opts`                                                                       | [][operations.Option](../../models/operations/option.md)                     | :heavy_minus_sign:                                                           | The options for this request.                                                |

### Response

**[*operations.UpdateTenantDestinationResponse](../../models/operations/updatetenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
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

## Delete

Deletes a specific destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="deleteTenantDestination" method="delete" path="/{tenant_id}/destinations/{destination_id}" -->
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
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Destinations.Delete(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.SuccessResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `destinationID`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.DeleteTenantDestinationResponse](../../models/operations/deletetenantdestinationresponse.md), error**

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

## Enable

Enables a previously disabled destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="enableTenantDestination" method="put" path="/{tenant_id}/destinations/{destination_id}/enable" -->
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
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Destinations.Enable(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `destinationID`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.EnableTenantDestinationResponse](../../models/operations/enabletenantdestinationresponse.md), error**

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

## Disable

Disables a previously enabled destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="disableTenantDestination" method="put" path="/{tenant_id}/destinations/{destination_id}/disable" -->
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
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Destinations.Disable(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `destinationID`                                                       | *string*                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.DisableTenantDestinationResponse](../../models/operations/disabletenantdestinationresponse.md), error**

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