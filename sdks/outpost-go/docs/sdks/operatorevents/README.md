# OperatorEvents

## Overview

Operator events are Outpost's own lifecycle and alerting stream: delivery failures, destinations being auto-disabled, retry exhaustion and tenant subscription changes. They are about the deployment, not about a tenant's traffic, so they are delivered to **operator event destinations** rather than to tenant destinations. An operator event destination is deployment-wide, is not owned by any tenant you create, and has the same shape as a tenant destination: any destination type listed by `GET /operator-events/destination-types`, with that type's `config` and `credentials`.

The Operator Events API is only available on managed Outpost, and requires the Admin API Key. Self-hosted deployments configure the same thing through environment variables and a single sink. See the [Operator Events](https://hookdeck.com/docs/outpost/features/operator-events) feature page.

**Available topics**

| Topic | Trigger |
|-------|---------|
| `alert.destination.consecutive_failure` | Consecutive failure count reaches 50%, 70%, 90%, or 100% of `ALERT_CONSECUTIVE_FAILURE_COUNT` |
| `alert.destination.disabled` | Destination auto-disabled at the 100% failure threshold |
| `alert.attempt.exhausted_retries` | Delivery exhausts all retry attempts |
| `attempt.success` | Every successful delivery attempt |
| `attempt.failed` | Every failed delivery attempt, including retries |
| `tenant.subscription.updated` | A destination was created, updated or deleted and the tenant's topics or destination count changed |

A destination may subscribe to every topic with `["*"]`, but `attempt.success` and `attempt.failed` fire once per delivery attempt and will dominate volume. Prefer an explicit topic list.

**Topics are derived from these destinations.** The union of every enabled operator event destination's `topics` is what the deployment subscribes to, which is why `PATCH /config` rejects `OPERATOR_EVENTS_TOPICS` with a 422: change the destinations and the configuration follows.


### Available Operations

* [ListDestinationTypes](#listdestinationtypes) - List Operator Event Destination Type Schemas
* [ListDestinations](#listdestinations) - List Operator Event Destinations
* [CreateDestination](#createdestination) - Create Operator Event Destination
* [GetDestination](#getdestination) - Get Operator Event Destination
* [UpdateDestination](#updatedestination) - Update Operator Event Destination
* [DeleteDestination](#deletedestination) - Delete Operator Event Destination
* [EnableDestination](#enabledestination) - Enable Operator Event Destination
* [DisableDestination](#disabledestination) - Disable Operator Event Destination
* [ListEvents](#listevents) - List Operator Events
* [GetEvent](#getevent) - Get Operator Event
* [ListEventAttempts](#listeventattempts) - List Attempts for an Operator Event
* [ListAttempts](#listattempts) - List Operator Event Attempts
* [GetAttempt](#getattempt) - Get Operator Event Attempt
* [Retry](#retry) - Retry Operator Event Delivery

## ListDestinationTypes

Returns the destination types an operator event destination may use, with the `config`
and `credentials` fields each type takes.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="listOperatorEventDestinationTypeSchemas" method="get" path="/operator-events/destination-types" example="OperatorEventDestinationTypesExample" -->
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

    res, err := s.OperatorEvents.ListDestinationTypes(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.DestinationTypeSchemas != nil {
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

**[*operations.ListOperatorEventDestinationTypeSchemasResponse](../../models/operations/listoperatoreventdestinationtypeschemasresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## ListDestinations

Returns every operator event destination configured for this deployment.

The response is a plain array, not a paginated result. A project that has never had an
operator event destination may answer 404 `tenant not found` instead of an empty array.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="listOperatorEventDestinations" method="get" path="/operator-events/destinations" example="OperatorEventDestinationsExample" -->
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

    res, err := s.OperatorEvents.ListDestinations(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.Destinations != nil {
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

**[*operations.ListOperatorEventDestinationsResponse](../../models/operations/listoperatoreventdestinationsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## CreateDestination

Creates an operator event destination of any type listed by
`GET /operator-events/destination-types`. `config` and `credentials` take the same fields
as a tenant destination of that type.

For a `webhook` destination, set `credentials.secret` to supply the signing secret. When
it is omitted, Outpost generates one. Either way the secret is returned in `credentials`.

Creating or changing a destination changes which topics the deployment subscribes to,
which is why `OPERATOR_EVENTS_TOPICS` cannot be set through `PATCH /config`.

This endpoint is only available on managed Outpost.


### Example Usage: Hookdeck

<!-- UsageSnippet language="go" operationID="createOperatorEventDestination" method="post" path="/operator-events/destinations" example="Hookdeck" -->
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

    res, err := s.OperatorEvents.CreateDestination(ctx, components.OperatorEventDestinationCreate{
        Type: "hookdeck",
        Topics: []string{
            "*",
        },
        Config: map[string]string{

        },
        Credentials: map[string]string{
            "token": "hd_token_...",
        },
    })
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
            case components.DestinationUnionTypeCloudflareQueues:
                // res.Destination.DestinationCloudflareQueues is populated
            case components.DestinationUnionTypeAwsEventbridge:
                // res.Destination.DestinationAWSEventBridge is populated
        }

    }
}
```
### Example Usage: Webhook

<!-- UsageSnippet language="go" operationID="createOperatorEventDestination" method="post" path="/operator-events/destinations" example="Webhook" -->
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

    res, err := s.OperatorEvents.CreateDestination(ctx, components.OperatorEventDestinationCreate{
        Type: "webhook",
        Topics: []string{
            "alert.destination.disabled",
            "alert.destination.consecutive_failure",
        },
        Config: map[string]string{
            "url": "https://alerts.acme.com/outpost",
        },
    })
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
            case components.DestinationUnionTypeCloudflareQueues:
                // res.Destination.DestinationCloudflareQueues is populated
            case components.DestinationUnionTypeAwsEventbridge:
                // res.Destination.DestinationAWSEventBridge is populated
        }

    }
}
```

### Parameters

| Parameter                                                                                              | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `ctx`                                                                                                  | [context.Context](https://pkg.go.dev/context#Context)                                                  | :heavy_check_mark:                                                                                     | The context to use for the request.                                                                    |
| `request`                                                                                              | [components.OperatorEventDestinationCreate](../../models/components/operatoreventdestinationcreate.md) | :heavy_check_mark:                                                                                     | The request object to use for the request.                                                             |
| `opts`                                                                                                 | [][operations.Option](../../models/operations/option.md)                                               | :heavy_minus_sign:                                                                                     | The options for this request.                                                                          |

### Response

**[*operations.CreateOperatorEventDestinationResponse](../../models/operations/createoperatoreventdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.BadRequestError     | 400                           | application/json              |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.APIErrorResponse    | 422                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## GetDestination

Retrieves a single operator event destination.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="getOperatorEventDestination" method="get" path="/operator-events/destinations/{destination_id}" -->
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

    res, err := s.OperatorEvents.GetDestination(ctx, "<id>")
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
            case components.DestinationUnionTypeCloudflareQueues:
                // res.Destination.DestinationCloudflareQueues is populated
            case components.DestinationUnionTypeAwsEventbridge:
                // res.Destination.DestinationAWSEventBridge is populated
        }

    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `destinationID`                                          | `string`                                                 | :heavy_check_mark:                                       | The ID of the operator event destination.                |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.GetOperatorEventDestinationResponse](../../models/operations/getoperatoreventdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## UpdateDestination

Updates an operator event destination. `topics` uses full-replacement semantics: the list
sent becomes the subscription.

`config` and `credentials` take the fields of the destination's type; fields that are not
sent keep their value. The type itself cannot be changed.

To change the signing secret of a `webhook` destination, set `credentials.secret`, or send
`credentials.rotate_secret: true` to have Outpost generate a new one. On rotation the
current secret stays valid as `previous_secret`, for 24 hours by default.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="updateOperatorEventDestination" method="patch" path="/operator-events/destinations/{destination_id}" -->
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

    res, err := s.OperatorEvents.UpdateDestination(ctx, "<id>", components.OperatorEventDestinationUpdate{
        Topics: []string{
            "alert.destination.disabled",
            "alert.destination.consecutive_failure",
            "alert.attempt.exhausted_retries",
        },
    })
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
            case components.DestinationUnionTypeCloudflareQueues:
                // res.Destination.DestinationCloudflareQueues is populated
            case components.DestinationUnionTypeAwsEventbridge:
                // res.Destination.DestinationAWSEventBridge is populated
        }

    }
}
```

### Parameters

| Parameter                                                                                              | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `ctx`                                                                                                  | [context.Context](https://pkg.go.dev/context#Context)                                                  | :heavy_check_mark:                                                                                     | The context to use for the request.                                                                    |
| `destinationID`                                                                                        | `string`                                                                                               | :heavy_check_mark:                                                                                     | The ID of the operator event destination.                                                              |
| `body`                                                                                                 | [components.OperatorEventDestinationUpdate](../../models/components/operatoreventdestinationupdate.md) | :heavy_check_mark:                                                                                     | N/A                                                                                                    |
| `opts`                                                                                                 | [][operations.Option](../../models/operations/option.md)                                               | :heavy_minus_sign:                                                                                     | The options for this request.                                                                          |

### Response

**[*operations.UpdateOperatorEventDestinationResponse](../../models/operations/updateoperatoreventdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.BadRequestError     | 400                           | application/json              |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.APIErrorResponse    | 422                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## DeleteDestination

Deletes an operator event destination. Deleting the last one leaves the deployment with no
operator event delivery, and therefore silent.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="deleteOperatorEventDestination" method="delete" path="/operator-events/destinations/{destination_id}" example="OperatorEventDestinationDeleted" -->
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

    res, err := s.OperatorEvents.DeleteDestination(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.SuccessResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `destinationID`                                          | `string`                                                 | :heavy_check_mark:                                       | The ID of the operator event destination.                |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.DeleteOperatorEventDestinationResponse](../../models/operations/deleteoperatoreventdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## EnableDestination

Enables a previously disabled operator event destination. Returns the destination with
`disabled_at` set to null.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="enableOperatorEventDestination" method="put" path="/operator-events/destinations/{destination_id}/enable" -->
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

    res, err := s.OperatorEvents.EnableDestination(ctx, "<id>")
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
            case components.DestinationUnionTypeCloudflareQueues:
                // res.Destination.DestinationCloudflareQueues is populated
            case components.DestinationUnionTypeAwsEventbridge:
                // res.Destination.DestinationAWSEventBridge is populated
        }

    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `destinationID`                                          | `string`                                                 | :heavy_check_mark:                                       | The ID of the operator event destination.                |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.EnableOperatorEventDestinationResponse](../../models/operations/enableoperatoreventdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## DisableDestination

Disables an operator event destination. A disabled destination receives nothing, so
disabling the only one leaves the deployment configured and silent.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="disableOperatorEventDestination" method="put" path="/operator-events/destinations/{destination_id}/disable" -->
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

    res, err := s.OperatorEvents.DisableDestination(ctx, "<id>")
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
            case components.DestinationUnionTypeCloudflareQueues:
                // res.Destination.DestinationCloudflareQueues is populated
            case components.DestinationUnionTypeAwsEventbridge:
                // res.Destination.DestinationAWSEventBridge is populated
        }

    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `destinationID`                                          | `string`                                                 | :heavy_check_mark:                                       | The ID of the operator event destination.                |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.DisableOperatorEventDestinationResponse](../../models/operations/disableoperatoreventdestinationresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## ListEvents

Returns the 100 most recent operator events emitted by this deployment, newest first.
Paging is not available yet: this operation takes no query parameters, so
`pagination.next` cannot be passed back.

Each row is an Outpost event whose `topic` is the operator event topic, whose
`matched_destination_ids` are operator event destinations, and whose `tenant_id` is the
ID of your Outpost project. The topic-specific payload is nested under `data`, which
carries the same envelope the sink receives: `id`, `topic`, `time`, `tenant_id` and a
`data` object described on the
[Operator Events](https://hookdeck.com/docs/outpost/features/operator-events) feature page.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="listOperatorEvents" method="get" path="/operator-events/events" example="OperatorEventsListExample" -->
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

    res, err := s.OperatorEvents.ListEvents(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.EventPaginatedResult != nil {
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

**[*operations.ListOperatorEventsResponse](../../models/operations/listoperatoreventsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## GetEvent

Retrieves a single operator event.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="getOperatorEvent" method="get" path="/operator-events/events/{event_id}" -->
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

    res, err := s.OperatorEvents.GetEvent(ctx, "<id>")
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
| `eventID`                                                | `string`                                                 | :heavy_check_mark:                                       | The ID of the operator event.                            |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.GetOperatorEventResponse](../../models/operations/getoperatoreventresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## ListEventAttempts

Returns the 100 most recent delivery attempts made for one operator event, newest first.
Paging is not available yet: this operation takes no query parameters, so
`pagination.next` cannot be passed back.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="listOperatorEventAttemptsByEvent" method="get" path="/operator-events/events/{event_id}/attempts" -->
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

    res, err := s.OperatorEvents.ListEventAttempts(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.AttemptPaginatedResult != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `eventID`                                                | `string`                                                 | :heavy_check_mark:                                       | The ID of the operator event.                            |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.ListOperatorEventAttemptsByEventResponse](../../models/operations/listoperatoreventattemptsbyeventresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## ListAttempts

Returns the delivery attempts made to operator event destinations, newest first.

The response holds one page of up to `limit` attempts. Paging is not available yet:
`pagination.next` cannot be passed back.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="listOperatorEventAttempts" method="get" path="/operator-events/attempts" example="OperatorEventAttemptsListExample" -->
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

    res, err := s.OperatorEvents.ListAttempts(ctx, operations.ListOperatorEventAttemptsRequest{})
    if err != nil {
        log.Fatal(err)
    }
    if res.AttemptPaginatedResult != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                                                  | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `ctx`                                                                                                      | [context.Context](https://pkg.go.dev/context#Context)                                                      | :heavy_check_mark:                                                                                         | The context to use for the request.                                                                        |
| `request`                                                                                                  | [operations.ListOperatorEventAttemptsRequest](../../models/operations/listoperatoreventattemptsrequest.md) | :heavy_check_mark:                                                                                         | The request object to use for the request.                                                                 |
| `opts`                                                                                                     | [][operations.Option](../../models/operations/option.md)                                                   | :heavy_minus_sign:                                                                                         | The options for this request.                                                                              |

### Response

**[*operations.ListOperatorEventAttemptsResponse](../../models/operations/listoperatoreventattemptsresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.BadRequestError     | 400                           | application/json              |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.APIErrorResponse    | 422                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## GetAttempt

Retrieves a single operator event delivery attempt.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="getOperatorEventAttempt" method="get" path="/operator-events/attempts/{attempt_id}" -->
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

    res, err := s.OperatorEvents.GetAttempt(ctx, "<id>")
    if err != nil {
        log.Fatal(err)
    }
    if res.Attempt != nil {
        switch res.Attempt.Event.Type {
            case components.EventUnionTypeEventFull:
                // res.Attempt.Event.EventFull is populated
            case components.EventUnionTypeEventSummary:
                // res.Attempt.Event.EventSummary is populated
        }

    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `attemptID`                                              | `string`                                                 | :heavy_check_mark:                                       | The ID of the operator event delivery attempt.           |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.GetOperatorEventAttemptResponse](../../models/operations/getoperatoreventattemptresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## Retry

Redelivers an operator event to one operator event destination. The event must exist, and
the destination must be enabled, match the event's topic and filter, and already have a
delivery attempt for the event.

Returns 404 `event not found` if the event does not exist. Returns 404
`destination not found` if the destination does not exist or has been deleted.

Unlike `POST /retry`, which answers `202`, this answers `200` with `{"success": true}`.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="go" operationID="retryOperatorEvent" method="post" path="/operator-events/retry" example="OperatorEventRetryAccepted" -->
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

    res, err := s.OperatorEvents.Retry(ctx, components.OperatorEventRetryRequest{
        EventID: "F1JKYUvykB5grOoE6Bnm6jztlF",
        DestinationID: "des_12345",
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

| Parameter                                                                                    | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `ctx`                                                                                        | [context.Context](https://pkg.go.dev/context#Context)                                        | :heavy_check_mark:                                                                           | The context to use for the request.                                                          |
| `request`                                                                                    | [components.OperatorEventRetryRequest](../../models/components/operatoreventretryrequest.md) | :heavy_check_mark:                                                                           | The request object to use for the request.                                                   |
| `opts`                                                                                       | [][operations.Option](../../models/operations/option.md)                                     | :heavy_minus_sign:                                                                           | The options for this request.                                                                |

### Response

**[*operations.RetryOperatorEventResponse](../../models/operations/retryoperatoreventresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.NotFoundError       | 404                           | application/json              |
| apierrors.APIErrorResponse    | 422                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |