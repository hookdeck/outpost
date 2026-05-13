# Destinations

## Overview

Destinations are the endpoints where events are sent. Each destination is associated with a tenant and can be configured to receive specific event topics.

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
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.list(tenant_id="<id>", type_=models.DestinationType.WEBHOOK)

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                                                                    | Type                                                                                                                                         | Required                                                                                                                                     | Description                                                                                                                                  |
| -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| `tenant_id`                                                                                                                                  | *str*                                                                                                                                        | :heavy_check_mark:                                                                                                                           | The ID of the tenant. Required when using AdminApiKey authentication.                                                                        |
| `type`                                                                                                                                       | [Optional[models.ListTenantDestinationsType]](../../models/listtenantdestinationstype.md)                                                    | :heavy_minus_sign:                                                                                                                           | Filter destinations by type(s). Use bracket notation for multiple values (e.g., `type[0]=webhook&type[1]=aws_sqs`).                          |
| `topics`                                                                                                                                     | [Optional[models.ListTenantDestinationsTopics]](../../models/listtenantdestinationstopics.md)                                                | :heavy_minus_sign:                                                                                                                           | Filter destinations by supported topic(s). Use bracket notation for multiple values (e.g., `topics[0]=user.created&topics[1]=user.deleted`). |
| `retries`                                                                                                                                    | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                             | :heavy_minus_sign:                                                                                                                           | Configuration to override the default retry behavior of the client.                                                                          |

### Response

**[List[models.Destination]](../../models/.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## create

Creates a new destination for the tenant. The request body structure depends on the `type`.

### Example Usage: WebhookCreateExample

<!-- UsageSnippet language="python" operationID="createTenantDestination" method="post" path="/tenants/{tenant_id}/destinations" example="WebhookCreateExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.create(tenant_id="<id>", body={
        "type": models.DestinationCreateWebhookType.WEBHOOK,
        "topics": [
            "user.created",
            "order.shipped",
        ],
        "config": {
            "url": "https://my-service.com/webhook/handler",
        },
    })

    # Handle response
    print(res)

```
### Example Usage: WebhookCreatedExample

<!-- UsageSnippet language="python" operationID="createTenantDestination" method="post" path="/tenants/{tenant_id}/destinations" example="WebhookCreatedExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.create(tenant_id="<id>", body={
        "id": "user-provided-id",
        "type": models.DestinationCreateRabbitMQType.RABBITMQ,
        "topics": models.TopicsEnum.WILDCARD_,
        "config": {
            "server_url": "localhost:5672",
            "exchange": "my-exchange",
            "tls": models.RabbitMQConfigTLS.FALSE,
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
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `body`                                                                | [models.DestinationCreate](../../models/destinationcreate.md)         | :heavy_check_mark:                                                    | N/A                                                                   |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get

Retrieves details for a specific destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantDestination" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}" example="WebhookGetExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.get(tenant_id="<id>", destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## update

Updates the configuration of an existing destination. The request body structure depends on the destination's `type`. Type itself cannot be updated. May return an OAuth redirect URL for certain types.

### Example Usage: DestinationUpdatedExample

<!-- UsageSnippet language="python" operationID="updateTenantDestination" method="patch" path="/tenants/{tenant_id}/destinations/{destination_id}" example="DestinationUpdatedExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.update(tenant_id="<id>", destination_id="<id>", body={
        "topics": models.TopicsEnum.WILDCARD_,
        "config": {
            "server_url": "localhost:5672",
            "exchange": "my-exchange",
            "tls": models.RabbitMQConfigTLS.FALSE,
        },
        "credentials": {
            "username": "guest",
            "password": "guest",
        },
    })

    # Handle response
    print(res)

```
### Example Usage: WebhookUpdateExample

<!-- UsageSnippet language="python" operationID="updateTenantDestination" method="patch" path="/tenants/{tenant_id}/destinations/{destination_id}" example="WebhookUpdateExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.update(tenant_id="<id>", destination_id="<id>", body={
        "topics": [
            "user.created",
        ],
        "config": {
            "url": "https://my-service.com/webhook/new-handler",
        },
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `body`                                                                | [models.DestinationUpdate](../../models/destinationupdate.md)         | :heavy_check_mark:                                                    | N/A                                                                   |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.UpdateTenantDestinationResponse](../../models/updatetenantdestinationresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## delete

Deletes a specific destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="deleteTenantDestination" method="delete" path="/tenants/{tenant_id}/destinations/{destination_id}" example="SuccessExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.delete(tenant_id="<id>", destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.SuccessResponse](../../models/successresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## enable

Enables a previously disabled destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="enableTenantDestination" method="put" path="/tenants/{tenant_id}/destinations/{destination_id}/enable" example="WebhookEnabledExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.enable(tenant_id="<id>", destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## disable

Disables a previously enabled destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="disableTenantDestination" method="put" path="/tenants/{tenant_id}/destinations/{destination_id}/disable" example="WebhookDisabledExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.disable(tenant_id="<id>", destination_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Destination](../../models/destination.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## list_attempts

Retrieves a paginated list of attempts scoped to a specific destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="listTenantDestinationAttempts" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}/attempts" example="DestinationAttemptsListExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.list_attempts(request={
        "tenant_id": "<id>",
        "destination_id": "<id>",
    })

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                                                           | Type                                                                                                | Required                                                                                            | Description                                                                                         |
| --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------- |
| `request`                                                                                           | [models.ListTenantDestinationAttemptsRequest](../../models/listtenantdestinationattemptsrequest.md) | :heavy_check_mark:                                                                                  | The request object to use for the request.                                                          |
| `retries`                                                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                    | :heavy_minus_sign:                                                                                  | Configuration to override the default retry behavior of the client.                                 |

### Response

**[models.ListTenantDestinationAttemptsResponse](../../models/listtenantdestinationattemptsresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get_attempt

Retrieves details for a specific attempt scoped to a destination.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantDestinationAttempt" method="get" path="/tenants/{tenant_id}/destinations/{destination_id}/attempts/{attempt_id}" example="DestinationAttemptExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.destinations.get_attempt(tenant_id="<id>", destination_id="<id>", attempt_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                                                                                                                                                                                                                                                         | Type                                                                                                                                                                                                                                                                                                                              | Required                                                                                                                                                                                                                                                                                                                          | Description                                                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tenant_id`                                                                                                                                                                                                                                                                                                                       | *str*                                                                                                                                                                                                                                                                                                                             | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                | The ID of the tenant. Required when using AdminApiKey authentication.                                                                                                                                                                                                                                                             |
| `destination_id`                                                                                                                                                                                                                                                                                                                  | *str*                                                                                                                                                                                                                                                                                                                             | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                | The ID of the destination.                                                                                                                                                                                                                                                                                                        |
| `attempt_id`                                                                                                                                                                                                                                                                                                                      | *str*                                                                                                                                                                                                                                                                                                                             | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                | The ID of the attempt.                                                                                                                                                                                                                                                                                                            |
| `include`                                                                                                                                                                                                                                                                                                                         | [Optional[models.GetTenantDestinationAttemptInclude]](../../models/gettenantdestinationattemptinclude.md)                                                                                                                                                                                                                         | :heavy_minus_sign:                                                                                                                                                                                                                                                                                                                | Fields to include in the response. Use bracket notation for multiple values (e.g., `include[0]=event&include[1]=response_data`).<br/>- `event`: Include event summary<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/>- `destination`: Include the full destination object<br/> |
| `retries`                                                                                                                                                                                                                                                                                                                         | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                                                                                                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                                                                                                                                                                                | Configuration to override the default retry behavior of the client.                                                                                                                                                                                                                                                               |

### Response

**[models.Attempt](../../models/attempt.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |