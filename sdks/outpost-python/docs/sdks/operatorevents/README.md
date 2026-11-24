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

* [list_destination_types](#list_destination_types) - List Operator Event Destination Type Schemas
* [list_destinations](#list_destinations) - List Operator Event Destinations
* [create_destination](#create_destination) - Create Operator Event Destination
* [get_destination](#get_destination) - Get Operator Event Destination
* [update_destination](#update_destination) - Update Operator Event Destination
* [delete_destination](#delete_destination) - Delete Operator Event Destination
* [enable_destination](#enable_destination) - Enable Operator Event Destination
* [disable_destination](#disable_destination) - Disable Operator Event Destination
* [list_events](#list_events) - List Operator Events
* [get_event](#get_event) - Get Operator Event
* [list_event_attempts](#list_event_attempts) - List Attempts for an Operator Event
* [list_attempts](#list_attempts) - List Operator Event Attempts
* [get_attempt](#get_attempt) - Get Operator Event Attempt
* [retry](#retry) - Retry Operator Event Delivery

## list_destination_types

Returns the destination types an operator event destination may use, with the `config`
and `credentials` fields each type takes.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="listOperatorEventDestinationTypeSchemas" method="get" path="/operator-events/destination-types" example="OperatorEventDestinationTypesExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.list_destination_types()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[List[models.DestinationTypeSchema]](../../models/.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## list_destinations

Returns every operator event destination configured for this deployment.

The response is a plain array, not a paginated result. A project that has never had an
operator event destination may answer 404 `tenant not found` instead of an empty array.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="listOperatorEventDestinations" method="get" path="/operator-events/destinations" example="OperatorEventDestinationsExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.list_destinations()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[List[models.Destination]](../../models/.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## create_destination

Creates an operator event destination of any type listed by
`GET /operator-events/destination-types`. `config` and `credentials` take the same fields
as a tenant destination of that type.

For a `webhook` destination, set `credentials.secret` to supply the signing secret. When
it is omitted, Outpost generates one. Either way the secret is returned in `credentials`.

Creating or changing a destination changes which topics the deployment subscribes to,
which is why `OPERATOR_EVENTS_TOPICS` cannot be set through `PATCH /config`.

This endpoint is only available on managed Outpost.


### Example Usage: Hookdeck

<!-- UsageSnippet language="python" operationID="createOperatorEventDestination" method="post" path="/operator-events/destinations" example="Hookdeck" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.create_destination(request={
        "type": "hookdeck",
        "topics": [
            "*",
        ],
        "config": {

        },
        "credentials": {
            "token": "hd_token_...",
        },
    })

    # Handle response
    print(res)

```
### Example Usage: Webhook

<!-- UsageSnippet language="python" operationID="createOperatorEventDestination" method="post" path="/operator-events/destinations" example="Webhook" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.create_destination(request={
        "type": "webhook",
        "topics": [
            "alert.destination.disabled",
            "alert.destination.consecutive_failure",
        ],
        "config": {
            "url": "https://alerts.acme.com/outpost",
        },
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                               | Type                                                                                    | Required                                                                                | Description                                                                             |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `request`                                                                               | [models.OperatorEventDestinationCreate](../../models/operatoreventdestinationcreate.md) | :heavy_check_mark:                                                                      | The request object to use for the request.                                              |
| `retries`                                                                               | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                        | :heavy_minus_sign:                                                                      | Configuration to override the default retry behavior of the client.                     |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get_destination

Retrieves a single operator event destination.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="getOperatorEventDestination" method="get" path="/operator-events/destinations/{destination_id}" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.get_destination(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `destination_id`                                                    | *str*                                                               | :heavy_check_mark:                                                  | The ID of the operator event destination.                           |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## update_destination

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

<!-- UsageSnippet language="python" operationID="updateOperatorEventDestination" method="patch" path="/operator-events/destinations/{destination_id}" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.update_destination(destination_id="<id>", body={
        "topics": [
            "alert.destination.disabled",
            "alert.destination.consecutive_failure",
            "alert.attempt.exhausted_retries",
        ],
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                               | Type                                                                                    | Required                                                                                | Description                                                                             |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `destination_id`                                                                        | *str*                                                                                   | :heavy_check_mark:                                                                      | The ID of the operator event destination.                                               |
| `body`                                                                                  | [models.OperatorEventDestinationUpdate](../../models/operatoreventdestinationupdate.md) | :heavy_check_mark:                                                                      | N/A                                                                                     |
| `retries`                                                                               | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                        | :heavy_minus_sign:                                                                      | Configuration to override the default retry behavior of the client.                     |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## delete_destination

Deletes an operator event destination. Deleting the last one leaves the deployment with no
operator event delivery, and therefore silent.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="deleteOperatorEventDestination" method="delete" path="/operator-events/destinations/{destination_id}" example="OperatorEventDestinationDeleted" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.delete_destination(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `destination_id`                                                    | *str*                                                               | :heavy_check_mark:                                                  | The ID of the operator event destination.                           |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.SuccessResponse](../../models/successresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## enable_destination

Enables a previously disabled operator event destination. Returns the destination with
`disabled_at` set to null.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="enableOperatorEventDestination" method="put" path="/operator-events/destinations/{destination_id}/enable" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.enable_destination(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `destination_id`                                                    | *str*                                                               | :heavy_check_mark:                                                  | The ID of the operator event destination.                           |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## disable_destination

Disables an operator event destination. A disabled destination receives nothing, so
disabling the only one leaves the deployment configured and silent.

Returns 404 `destination not found` if no operator event destination has this ID, or
404 `tenant not found` if the project has never had one.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="disableOperatorEventDestination" method="put" path="/operator-events/destinations/{destination_id}/disable" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.disable_destination(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `destination_id`                                                    | *str*                                                               | :heavy_check_mark:                                                  | The ID of the operator event destination.                           |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## list_events

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

<!-- UsageSnippet language="python" operationID="listOperatorEvents" method="get" path="/operator-events/events" example="OperatorEventsListExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.list_events()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.EventPaginatedResult](../../models/eventpaginatedresult.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get_event

Retrieves a single operator event.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="getOperatorEvent" method="get" path="/operator-events/events/{event_id}" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.get_event(event_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `event_id`                                                          | *str*                                                               | :heavy_check_mark:                                                  | The ID of the operator event.                                       |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.Event](../../models/event.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## list_event_attempts

Returns the 100 most recent delivery attempts made for one operator event, newest first.
Paging is not available yet: this operation takes no query parameters, so
`pagination.next` cannot be passed back.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="listOperatorEventAttemptsByEvent" method="get" path="/operator-events/events/{event_id}/attempts" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.list_event_attempts(event_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `event_id`                                                          | *str*                                                               | :heavy_check_mark:                                                  | The ID of the operator event.                                       |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.AttemptPaginatedResult](../../models/attemptpaginatedresult.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## list_attempts

Returns the delivery attempts made to operator event destinations, newest first.

The response holds one page of up to `limit` attempts. Paging is not available yet:
`pagination.next` cannot be passed back.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="listOperatorEventAttempts" method="get" path="/operator-events/attempts" example="OperatorEventAttemptsListExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.list_attempts(request={})

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                   | Type                                                                                        | Required                                                                                    | Description                                                                                 |
| ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------- |
| `request`                                                                                   | [models.ListOperatorEventAttemptsRequest](../../models/listoperatoreventattemptsrequest.md) | :heavy_check_mark:                                                                          | The request object to use for the request.                                                  |
| `retries`                                                                                   | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                            | :heavy_minus_sign:                                                                          | Configuration to override the default retry behavior of the client.                         |

### Response

**[models.AttemptPaginatedResult](../../models/attemptpaginatedresult.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get_attempt

Retrieves a single operator event delivery attempt.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="getOperatorEventAttempt" method="get" path="/operator-events/attempts/{attempt_id}" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.get_attempt(attempt_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `attempt_id`                                                        | *str*                                                               | :heavy_check_mark:                                                  | The ID of the operator event delivery attempt.                      |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.Attempt](../../models/attempt.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## retry

Redelivers an operator event to one operator event destination. The event must exist, and
the destination must be enabled, match the event's topic and filter, and already have a
delivery attempt for the event.

Returns 404 `event not found` if the event does not exist. Returns 404
`destination not found` if the destination does not exist or has been deleted.

Unlike `POST /retry`, which answers `202`, this answers `200` with `{"success": true}`.

This endpoint is only available on managed Outpost.


### Example Usage

<!-- UsageSnippet language="python" operationID="retryOperatorEvent" method="post" path="/operator-events/retry" example="OperatorEventRetryAccepted" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.operator_events.retry(request={
        "event_id": "F1JKYUvykB5grOoE6Bnm6jztlF",
        "destination_id": "des_12345",
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                     | Type                                                                          | Required                                                                      | Description                                                                   |
| ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| `request`                                                                     | [models.OperatorEventRetryRequest](../../models/operatoreventretryrequest.md) | :heavy_check_mark:                                                            | The request object to use for the request.                                    |
| `retries`                                                                     | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)              | :heavy_minus_sign:                                                            | Configuration to override the default retry behavior of the client.           |

### Response

**[models.SuccessResponse](../../models/successresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |