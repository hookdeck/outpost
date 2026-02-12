# Destinations

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

* [list](#list) - List Destinations
* [create](#create) - Create Destination
* [get](#get) - Get Destination
* [update](#update) - Update Destination
* [delete](#delete) - Delete Destination
* [enable](#enable) - Enable Destination
* [disable](#disable) - Disable Destination
* [list_attempts](#list_attempts) - List Destination Attempts
* [get_attempt](#get_attempt) - Get Destination Attempt

## list

Return a list of the destinations for the tenant. The endpoint is not paged.

### Example Usage

<!-- UsageSnippet language="python" operationID="listTenantDestinations" method="get" path="/tenants/{tenant_id}/destinations" example="DestinationsListExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.list(type_=models.DestinationType.WEBHOOK)

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                     | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `tenant_id`                                                                                   | *Optional[str]*                                                                               | :heavy_minus_sign:                                                                            | The ID of the tenant. Required when using AdminApiKey authentication.                         |
| `type`                                                                                        | [Optional[models.ListTenantDestinationsType]](../../models/listtenantdestinationstype.md)     | :heavy_minus_sign:                                                                            | Filter destinations by type(s).                                                               |
| `topics`                                                                                      | [Optional[models.ListTenantDestinationsTopics]](../../models/listtenantdestinationstopics.md) | :heavy_minus_sign:                                                                            | Filter destinations by supported topic(s).                                                    |
| `retries`                                                                                     | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                              | :heavy_minus_sign:                                                                            | Configuration to override the default retry behavior of the client.                           |

### Response

**[List[models.Destination]](../../models/.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## create

Creates a new destination for the tenant. The request body structure depends on the `type`.

### Example Usage

<!-- UsageSnippet language="python" operationID="createTenantDestination" method="post" path="/tenants/{tenant_id}/destinations" example="WebhookCreatedExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.create(params={
        "id": "user-provided-id",
        "type": models.DestinationCreateRabbitMQType.RABBITMQ,
        "topics": models.TopicsEnum.WILDCARD_,
        "config": {
            "server_url": "localhost:5672",
            "exchange": "my-exchange",
            "tls": models.TLS.FALSE,
        },
        "credentials": {
            "username": "guest",
            "password": "guest",
        },
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `params`                                                              | [models.DestinationCreate](../../models/destinationcreate.md)         | :heavy_check_mark:                                                    | N/A                                                                   |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## get

Retrieves details for a specific destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantDestination" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}" example="WebhookGetExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.get(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## update

Updates the configuration of an existing destination. The request body structure depends on the destination's `type`. Type itself cannot be updated. May return an OAuth redirect URL for certain types.

### Example Usage

<!-- UsageSnippet language="python" operationID="updateTenantDestination" method="patch" path="/tenants/{tenant_id}/destinations/{destination_id}" example="DestinationUpdatedExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.update(destination_id="<id>", params={
        "topics": models.TopicsEnum.WILDCARD_,
        "config": {
            "server_url": "localhost:5672",
            "exchange": "my-exchange",
            "tls": models.TLS.FALSE,
        },
        "credentials": {
            "username": "guest",
            "password": "guest",
        },
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `params`                                                              | [models.DestinationUpdate](../../models/destinationupdate.md)         | :heavy_check_mark:                                                    | N/A                                                                   |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.UpdateTenantDestinationResponse](../../models/updatetenantdestinationresponse.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## delete

Deletes a specific destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="deleteTenantDestination" method="delete" path="/tenants/{tenant_id}/destinations/{destination_id}" example="SuccessExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.delete(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.SuccessResponse](../../models/successresponse.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## enable

Enables a previously disabled destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="enableTenantDestination" method="put" path="/tenants/{tenant_id}/destinations/{destination_id}/enable" example="WebhookEnabledExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.enable(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## disable

Disables a previously enabled destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="disableTenantDestination" method="put" path="/tenants/{tenant_id}/destinations/{destination_id}/disable" example="WebhookDisabledExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.disable(destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## list_attempts

Retrieves a paginated list of attempts scoped to a specific destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="listTenantDestinationAttempts" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}/attempts" example="DestinationAttemptsListExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.list_attempts(destination_id="<id>", limit=100, order_by=models.ListTenantDestinationAttemptsOrderBy.TIME, direction=models.ListTenantDestinationAttemptsDir.DESC)

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                                                                                                                                                                                                                                          | Type                                                                                                                                                                                                                                                                               | Required                                                                                                                                                                                                                                                                           | Description                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `destination_id`                                                                                                                                                                                                                                                                   | *str*                                                                                                                                                                                                                                                                              | :heavy_check_mark:                                                                                                                                                                                                                                                                 | The ID of the destination.                                                                                                                                                                                                                                                         |
| `tenant_id`                                                                                                                                                                                                                                                                        | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | The ID of the tenant. Required when using AdminApiKey authentication.                                                                                                                                                                                                              |
| `event_id`                                                                                                                                                                                                                                                                         | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event ID.                                                                                                                                                                                                                                                       |
| `status`                                                                                                                                                                                                                                                                           | [Optional[models.ListTenantDestinationAttemptsStatus]](../../models/listtenantdestinationattemptsstatus.md)                                                                                                                                                                        | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by status.                                                                                                                                                                                                                                                         |
| `topic`                                                                                                                                                                                                                                                                            | [Optional[models.ListTenantDestinationAttemptsTopic]](../../models/listtenantdestinationattemptstopic.md)                                                                                                                                                                          | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event topic(s). Can be specified multiple times or comma-separated.                                                                                                                                                                                             |
| `time_gte`                                                                                                                                                                                                                                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects)                                                                                                                                                                                                               | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event time >= value (RFC3339 or YYYY-MM-DD format).                                                                                                                                                                                                             |
| `time_lte`                                                                                                                                                                                                                                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects)                                                                                                                                                                                                               | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event time <= value (RFC3339 or YYYY-MM-DD format).                                                                                                                                                                                                             |
| `limit`                                                                                                                                                                                                                                                                            | *Optional[int]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Number of items per page (default 100, max 1000).                                                                                                                                                                                                                                  |
| `next_cursor`                                                                                                                                                                                                                                                                      | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Cursor for next page of results.                                                                                                                                                                                                                                                   |
| `prev_cursor`                                                                                                                                                                                                                                                                      | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Cursor for previous page of results.                                                                                                                                                                                                                                               |
| `include`                                                                                                                                                                                                                                                                          | [Optional[models.ListTenantDestinationAttemptsInclude]](../../models/listtenantdestinationattemptsinclude.md)                                                                                                                                                                      | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Fields to include in the response. Can be specified multiple times or comma-separated.<br/>- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/> |
| `order_by`                                                                                                                                                                                                                                                                         | [Optional[models.ListTenantDestinationAttemptsOrderBy]](../../models/listtenantdestinationattemptsorderby.md)                                                                                                                                                                      | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Field to sort by.                                                                                                                                                                                                                                                                  |
| `direction`                                                                                                                                                                                                                                                                        | [Optional[models.ListTenantDestinationAttemptsDir]](../../models/listtenantdestinationattemptsdir.md)                                                                                                                                                                              | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Sort direction.                                                                                                                                                                                                                                                                    |
| `retries`                                                                                                                                                                                                                                                                          | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                                                                                                                                                                   | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Configuration to override the default retry behavior of the client.                                                                                                                                                                                                                |

### Response

**[models.ListTenantDestinationAttemptsResponse](../../models/listtenantdestinationattemptsresponse.md)**

### Errors

| Error Type              | Status Code             | Content Type            |
| ----------------------- | ----------------------- | ----------------------- |
| errors.APIErrorResponse | 422                     | application/json        |
| errors.APIError         | 4XX, 5XX                | \*/\*                   |

## get_attempt

Retrieves details for a specific attempt scoped to a destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantDestinationAttempt" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}/attempts/{attempt_id}" example="DestinationAttemptExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.destinations.get_attempt(destination_id="<id>", attempt_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                                                                                                                                                          | Type                                                                                                                                                                                                                               | Required                                                                                                                                                                                                                           | Description                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `destination_id`                                                                                                                                                                                                                   | *str*                                                                                                                                                                                                                              | :heavy_check_mark:                                                                                                                                                                                                                 | The ID of the destination.                                                                                                                                                                                                         |
| `attempt_id`                                                                                                                                                                                                                       | *str*                                                                                                                                                                                                                              | :heavy_check_mark:                                                                                                                                                                                                                 | The ID of the attempt.                                                                                                                                                                                                             |
| `tenant_id`                                                                                                                                                                                                                        | *Optional[str]*                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                 | The ID of the tenant. Required when using AdminApiKey authentication.                                                                                                                                                              |
| `include`                                                                                                                                                                                                                          | [Optional[models.GetTenantDestinationAttemptInclude]](../../models/gettenantdestinationattemptinclude.md)                                                                                                                          | :heavy_minus_sign:                                                                                                                                                                                                                 | Fields to include in the response. Can be specified multiple times or comma-separated.<br/>- `event`: Include event summary<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/> |
| `retries`                                                                                                                                                                                                                          | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                                                                                                                   | :heavy_minus_sign:                                                                                                                                                                                                                 | Configuration to override the default retry behavior of the client.                                                                                                                                                                |

### Response

**[models.Attempt](../../models/attempt.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |