# Destinations

## Overview

Destinations are the endpoints where events are sent. Each destination is associated with a tenant and can be configured to receive specific event topics.

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
* [ListAttempts](#listattempts) - List Destination Attempts
* [GetAttempt](#getattempt) - Get Destination Attempt

## List

Return a list of the destinations for the tenant. The endpoint is not paged.

### Example Usage

<!-- UsageSnippet language="go" operationID="listTenantDestinations" method="get" path="/tenants/{tenant_id}/destinations" example="DestinationsListExample" -->
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

    res, err := s.Destinations.List(ctx, "<id>", []components.DestinationType{
        components.DestinationTypeWebhook,
    }, nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Destinations != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                                                                                    | Type                                                                                                                                         | Required                                                                                                                                     | Description                                                                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                                                                        | [context.Context](https://pkg.go.dev/context#Context)                                                                                        | :heavy_check_mark:                                                                                                                           | The context to use for the request.                                                                                                          |
| `tenantID`                                                                                                                                   | `string`                                                                                                                                     | :heavy_check_mark:                                                                                                                           | The ID of the tenant. Required when using AdminApiKey authentication.                                                                        |
| `type_`                                                                                                                                      | [][components.DestinationType](../../models/components/destinationtype.md)                                                                   | :heavy_minus_sign:                                                                                                                           | Filter destinations by type(s). Use bracket notation for multiple values (e.g., `type[0]=webhook&type[1]=aws_sqs`).                          |
| `topics`                                                                                                                                     | []`string`                                                                                                                                   | :heavy_minus_sign:                                                                                                                           | Filter destinations by supported topic(s). Use bracket notation for multiple values (e.g., `topics[0]=user.created&topics[1]=user.deleted`). |
| `opts`                                                                                                                                       | [][operations.Option](../../models/operations/option.md)                                                                                     | :heavy_minus_sign:                                                                                                                           | The options for this request.                                                                                                                |

### Response

**[*operations.ListTenantDestinationsResponse](../../models/operations/listtenantdestinationsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## Create

Creates a new destination for the tenant. The request body structure depends on the `type`.

### Example Usage: WebhookCreateExample

<!-- UsageSnippet language="go" operationID="createTenantDestination" method="post" path="/tenants/{tenant_id}/destinations" example="WebhookCreateExample" -->
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

    res, err := s.Destinations.Create(ctx, "<id>", components.CreateDestinationCreateWebhook(
        components.DestinationCreateWebhook{
            Type: components.DestinationCreateWebhookTypeWebhook,
            Topics: components.CreateTopicsArrayOfStr(
                []string{
                    "user.created",
                    "order.shipped",
                },
            ),
            Config: components.WebhookConfig{
                URL: "https://my-service.com/webhook/handler",
            },
        },
    ))
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        switch res.Destination.Type {
            case components.DestinationUnionTypeWebhook:
                // res.Destination.DestinationWebhook is populated
            case components.DestinationUnionTypeAwsSqs:
                // res.Destination.DestinationAWSSQS is populated
            case components.DestinationUnionTypeRabbitmq:
                // res.Destination.DestinationRabbitMQ is populated
            case components.DestinationUnionTypeHookdeck:
                // res.Destination.DestinationHookdeck is populated
            case components.DestinationUnionTypeAwsKinesis:
                // res.Destination.DestinationAWSKinesis is populated
            case components.DestinationUnionTypeAzureServicebus:
                // res.Destination.DestinationAzureServiceBus is populated
            case components.DestinationUnionTypeAwsS3:
                // res.Destination.DestinationAwss3 is populated
            case components.DestinationUnionTypeGcpPubsub:
                // res.Destination.DestinationGCPPubSub is populated
            case components.DestinationUnionTypeKafka:
                // res.Destination.DestinationKafka is populated
        }

    }
}
```
### Example Usage: WebhookCreatedExample

<!-- UsageSnippet language="go" operationID="createTenantDestination" method="post" path="/tenants/{tenant_id}/destinations" example="WebhookCreatedExample" -->
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

    res, err := s.Destinations.Create(ctx, "<id>", components.CreateDestinationCreateRabbitmq(
        components.DestinationCreateRabbitMQ{
            ID: outpostgo.Pointer("user-provided-id"),
            Type: components.DestinationCreateRabbitMQTypeRabbitmq,
            Topics: components.CreateTopicsTopicsEnum(
                components.TopicsEnumWildcard,
            ),
            Config: components.RabbitMQConfig{
                ServerURL: "localhost:5672",
                Exchange: "my-exchange",
                TLS: components.RabbitMQConfigTLSFalse.ToPointer(),
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
        switch res.Destination.Type {
            case components.DestinationUnionTypeWebhook:
                // res.Destination.DestinationWebhook is populated
            case components.DestinationUnionTypeAwsSqs:
                // res.Destination.DestinationAWSSQS is populated
            case components.DestinationUnionTypeRabbitmq:
                // res.Destination.DestinationRabbitMQ is populated
            case components.DestinationUnionTypeHookdeck:
                // res.Destination.DestinationHookdeck is populated
            case components.DestinationUnionTypeAwsKinesis:
                // res.Destination.DestinationAWSKinesis is populated
            case components.DestinationUnionTypeAzureServicebus:
                // res.Destination.DestinationAzureServiceBus is populated
            case components.DestinationUnionTypeAwsS3:
                // res.Destination.DestinationAwss3 is populated
            case components.DestinationUnionTypeGcpPubsub:
                // res.Destination.DestinationGCPPubSub is populated
            case components.DestinationUnionTypeKafka:
                // res.Destination.DestinationKafka is populated
        }

    }
}
```

### Parameters

| Parameter                                                                    | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `ctx`                                                                        | [context.Context](https://pkg.go.dev/context#Context)                        | :heavy_check_mark:                                                           | The context to use for the request.                                          |
| `tenantID`                                                                   | `string`                                                                     | :heavy_check_mark:                                                           | The ID of the tenant. Required when using AdminApiKey authentication.        |
| `body`                                                                       | [components.DestinationCreate](../../models/components/destinationcreate.md) | :heavy_check_mark:                                                           | N/A                                                                          |
| `opts`                                                                       | [][operations.Option](../../models/operations/option.md)                     | :heavy_minus_sign:                                                           | The options for this request.                                                |

### Response

**[*operations.CreateTenantDestinationResponse](../../models/operations/createtenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.APIErrorResponse    | 422                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## Get

Retrieves details for a specific destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="getTenantDestination" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}" example="WebhookGetExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.Get(ctx, "<id>", "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        switch res.Destination.Type {
            case components.DestinationUnionTypeWebhook:
                // res.Destination.DestinationWebhook is populated
            case components.DestinationUnionTypeAwsSqs:
                // res.Destination.DestinationAWSSQS is populated
            case components.DestinationUnionTypeRabbitmq:
                // res.Destination.DestinationRabbitMQ is populated
            case components.DestinationUnionTypeHookdeck:
                // res.Destination.DestinationHookdeck is populated
            case components.DestinationUnionTypeAwsKinesis:
                // res.Destination.DestinationAWSKinesis is populated
            case components.DestinationUnionTypeAzureServicebus:
                // res.Destination.DestinationAzureServiceBus is populated
            case components.DestinationUnionTypeAwsS3:
                // res.Destination.DestinationAwss3 is populated
            case components.DestinationUnionTypeGcpPubsub:
                // res.Destination.DestinationGCPPubSub is populated
            case components.DestinationUnionTypeKafka:
                // res.Destination.DestinationKafka is populated
        }

    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | `string`                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationID`                                                       | `string`                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.GetTenantDestinationResponse](../../models/operations/gettenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## Update

Updates the configuration of an existing destination. The request body structure depends on the destination's `type`. Type itself cannot be updated. May return an OAuth redirect URL for certain types.

### Example Usage: DestinationUpdatedExample

<!-- UsageSnippet language="go" operationID="updateTenantDestination" method="patch" path="/tenants/{tenant_id}/destinations/{destination_id}" example="DestinationUpdatedExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.Update(ctx, "<id>", "<id>", components.CreateDestinationUpdateDestinationUpdateRabbitMQ(
        components.DestinationUpdateRabbitMQ{
            Topics: outpostgo.Pointer(components.CreateTopicsTopicsEnum(
                components.TopicsEnumWildcard,
            )),
            Config: &components.RabbitMQConfig{
                ServerURL: "localhost:5672",
                Exchange: "my-exchange",
                TLS: components.RabbitMQConfigTLSFalse.ToPointer(),
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
        switch res.OneOf.Type {
            case operations.UpdateTenantDestinationResponseBodyTypeDestination:
                // res.OneOf.Destination is populated
        }

    }
}
```
### Example Usage: WebhookUpdateExample

<!-- UsageSnippet language="go" operationID="updateTenantDestination" method="patch" path="/tenants/{tenant_id}/destinations/{destination_id}" example="WebhookUpdateExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.Update(ctx, "<id>", "<id>", components.CreateDestinationUpdateDestinationUpdateWebhook(
        components.DestinationUpdateWebhook{
            Topics: outpostgo.Pointer(components.CreateTopicsArrayOfStr(
                []string{
                    "user.created",
                },
            )),
            Config: &components.WebhookConfig{
                URL: "https://my-service.com/webhook/new-handler",
            },
        },
    ))
    if err != nil {
        log.Fatal(err)
    }
    if res.OneOf != nil {
        switch res.OneOf.Type {
            case operations.UpdateTenantDestinationResponseBodyTypeDestination:
                // res.OneOf.Destination is populated
        }

    }
}
```

### Parameters

| Parameter                                                                    | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `ctx`                                                                        | [context.Context](https://pkg.go.dev/context#Context)                        | :heavy_check_mark:                                                           | The context to use for the request.                                          |
| `tenantID`                                                                   | `string`                                                                     | :heavy_check_mark:                                                           | The ID of the tenant. Required when using AdminApiKey authentication.        |
| `destinationID`                                                              | `string`                                                                     | :heavy_check_mark:                                                           | The ID of the destination.                                                   |
| `body`                                                                       | [components.DestinationUpdate](../../models/components/destinationupdate.md) | :heavy_check_mark:                                                           | N/A                                                                          |
| `opts`                                                                       | [][operations.Option](../../models/operations/option.md)                     | :heavy_minus_sign:                                                           | The options for this request.                                                |

### Response

**[*operations.UpdateTenantDestinationResponse](../../models/operations/updatetenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.APIErrorResponse    | 422                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## Delete

Deletes a specific destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="deleteTenantDestination" method="delete" path="/tenants/{tenant_id}/destinations/{destination_id}" example="SuccessExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.Delete(ctx, "<id>", "<id>")
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
| `tenantID`                                                            | `string`                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationID`                                                       | `string`                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.DeleteTenantDestinationResponse](../../models/operations/deletetenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## Enable

Enables a previously disabled destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="enableTenantDestination" method="put" path="/tenants/{tenant_id}/destinations/{destination_id}/enable" example="WebhookEnabledExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.Enable(ctx, "<id>", "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        switch res.Destination.Type {
            case components.DestinationUnionTypeWebhook:
                // res.Destination.DestinationWebhook is populated
            case components.DestinationUnionTypeAwsSqs:
                // res.Destination.DestinationAWSSQS is populated
            case components.DestinationUnionTypeRabbitmq:
                // res.Destination.DestinationRabbitMQ is populated
            case components.DestinationUnionTypeHookdeck:
                // res.Destination.DestinationHookdeck is populated
            case components.DestinationUnionTypeAwsKinesis:
                // res.Destination.DestinationAWSKinesis is populated
            case components.DestinationUnionTypeAzureServicebus:
                // res.Destination.DestinationAzureServiceBus is populated
            case components.DestinationUnionTypeAwsS3:
                // res.Destination.DestinationAwss3 is populated
            case components.DestinationUnionTypeGcpPubsub:
                // res.Destination.DestinationGCPPubSub is populated
            case components.DestinationUnionTypeKafka:
                // res.Destination.DestinationKafka is populated
        }

    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | `string`                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationID`                                                       | `string`                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.EnableTenantDestinationResponse](../../models/operations/enabletenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## Disable

Disables a previously enabled destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="disableTenantDestination" method="put" path="/tenants/{tenant_id}/destinations/{destination_id}/disable" example="WebhookDisabledExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.Disable(ctx, "<id>", "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Destination != nil {
        switch res.Destination.Type {
            case components.DestinationUnionTypeWebhook:
                // res.Destination.DestinationWebhook is populated
            case components.DestinationUnionTypeAwsSqs:
                // res.Destination.DestinationAWSSQS is populated
            case components.DestinationUnionTypeRabbitmq:
                // res.Destination.DestinationRabbitMQ is populated
            case components.DestinationUnionTypeHookdeck:
                // res.Destination.DestinationHookdeck is populated
            case components.DestinationUnionTypeAwsKinesis:
                // res.Destination.DestinationAWSKinesis is populated
            case components.DestinationUnionTypeAzureServicebus:
                // res.Destination.DestinationAzureServiceBus is populated
            case components.DestinationUnionTypeAwsS3:
                // res.Destination.DestinationAwss3 is populated
            case components.DestinationUnionTypeGcpPubsub:
                // res.Destination.DestinationGCPPubSub is populated
            case components.DestinationUnionTypeKafka:
                // res.Destination.DestinationKafka is populated
        }

    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | `string`                                                              | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destinationID`                                                       | `string`                                                              | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.DisableTenantDestinationResponse](../../models/operations/disabletenantdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## ListAttempts

Retrieves a paginated list of attempts scoped to a specific destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="listTenantDestinationAttempts" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}/attempts" example="DestinationAttemptsListExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.ListAttempts(ctx, operations.ListTenantDestinationAttemptsRequest{
        TenantID: "<id>",
        DestinationID: "<id>",
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.AttemptPaginatedResult != nil {
        for {
            // handle items

            res, err = res.Next()

            if err != nil {
                // handle error
            }

            if res == nil {
                break
            }
        }
    }
}
```

### Parameters

| Parameter                                                                                                          | Type                                                                                                               | Required                                                                                                           | Description                                                                                                        |
| ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `ctx`                                                                                                              | [context.Context](https://pkg.go.dev/context#Context)                                                              | :heavy_check_mark:                                                                                                 | The context to use for the request.                                                                                |
| `request`                                                                                                          | [operations.ListTenantDestinationAttemptsRequest](../../models/operations/listtenantdestinationattemptsrequest.md) | :heavy_check_mark:                                                                                                 | The request object to use for the request.                                                                         |
| `opts`                                                                                                             | [][operations.Option](../../models/operations/option.md)                                                           | :heavy_minus_sign:                                                                                                 | The options for this request.                                                                                      |

### Response

**[*operations.ListTenantDestinationAttemptsResponse](../../models/operations/listtenantdestinationattemptsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## GetAttempt

Retrieves details for a specific attempt scoped to a destination.

### Example Usage

<!-- UsageSnippet language="go" operationID="getTenantDestinationAttempt" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}/attempts/{attempt_id}" example="DestinationAttemptExample" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Destinations.GetAttempt(ctx, "<id>", "<id>", "<id>", nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Attempt != nil {
        switch res.Attempt.Event.Type {
            case components.EventUnionTypeEventSummary:
                // res.Attempt.Event.EventSummary is populated
            case components.EventUnionTypeEventFull:
                // res.Attempt.Event.EventFull is populated
        }

    }
}
```

### Parameters

| Parameter                                                                                                                                                                                                                                                                                                                         | Type                                                                                                                                                                                                                                                                                                                              | Required                                                                                                                                                                                                                                                                                                                          | Description                                                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                                                                                                                                                                                                                                                             | [context.Context](https://pkg.go.dev/context#Context)                                                                                                                                                                                                                                                                             | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                | The context to use for the request.                                                                                                                                                                                                                                                                                               |
| `tenantID`                                                                                                                                                                                                                                                                                                                        | `string`                                                                                                                                                                                                                                                                                                                          | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                | The ID of the tenant. Required when using AdminApiKey authentication.                                                                                                                                                                                                                                                             |
| `destinationID`                                                                                                                                                                                                                                                                                                                   | `string`                                                                                                                                                                                                                                                                                                                          | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                | The ID of the destination.                                                                                                                                                                                                                                                                                                        |
| `attemptID`                                                                                                                                                                                                                                                                                                                       | `string`                                                                                                                                                                                                                                                                                                                          | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                | The ID of the attempt.                                                                                                                                                                                                                                                                                                            |
| `include`                                                                                                                                                                                                                                                                                                                         | []`string`                                                                                                                                                                                                                                                                                                                        | :heavy_minus_sign:                                                                                                                                                                                                                                                                                                                | Fields to include in the response. Use bracket notation for multiple values (e.g., `include[0]=event&include[1]=response_data`).<br/>- `event`: Include event summary<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/>- `destination`: Include the full destination object<br/> |
| `opts`                                                                                                                                                                                                                                                                                                                            | [][operations.Option](../../models/operations/option.md)                                                                                                                                                                                                                                                                          | :heavy_minus_sign:                                                                                                                                                                                                                                                                                                                | The options for this request.                                                                                                                                                                                                                                                                                                     |

### Response

**[*operations.GetTenantDestinationAttemptResponse](../../models/operations/gettenantdestinationattemptresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |