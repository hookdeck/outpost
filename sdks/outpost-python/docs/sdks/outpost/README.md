# Outpost SDK

## Overview

Outpost API: The Outpost API is a REST-based JSON API for managing tenants, destinations, and publishing events.

Outpost runs in two deployment models: **managed** (hosted by Hookdeck) and **self-hosted**. They differ in where the API is served and in which API key authenticates server-side calls. On managed Outpost, use a Hookdeck project API key from your Outpost project. On self-hosted Outpost, use the key set in the `API_KEY` environment variable. A few endpoints exist in one model only and say so in their description.


### Available Operations

* [publish](#publish) - Publish Event
* [retry](#retry) - Retry Event Delivery

## publish

Publishes an event to the specified topic, potentially routed to a specific destination. Requires Admin API Key.

### Example Usage

<!-- UsageSnippet language="python" operationID="publishEvent" method="post" path="/publish" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.publish(request={
        "id": "evt_abc123xyz789",
        "tenant_id": "tenant_123",
        "topic": "user.created",
        "eligible_for_retry": True,
        "metadata": {
            "source": "crm",
        },
        "data": {
            "user_id": "userid",
            "status": "active",
        },
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `request`                                                           | [models.PublishRequest](../../models/publishrequest.md)             | :heavy_check_mark:                                                  | The request object to use for the request.                          |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.PublishResponse](../../models/publishresponse.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.NotFoundError         | 404                          | application/json             |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.APIErrorResponse      | 409                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 413, 414, 415, 422, 431      | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## retry

Triggers a retry for delivering an event to a destination. The event must exist, and the destination must be enabled, match the event's topic and filter, and already have a delivery attempt for the event. A retry never sends an event to a destination that has no earlier attempt for it.

Returns 404 `event not found` if the event does not exist, or belongs to another tenant when authenticated with a Tenant JWT. Returns 404 `destination not found` if the destination does not exist for the event's tenant or has been deleted.

When authenticated with a Tenant JWT, only events belonging to that tenant can be retried.
When authenticated with Admin API Key, events from any tenant can be retried.


### Example Usage

<!-- UsageSnippet language="python" operationID="retryEvent" method="post" path="/retry" example="RetryAccepted" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.retry(request={
        "event_id": "evt_123",
        "destination_id": "des_456",
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `request`                                                           | [models.RetryRequest](../../models/retryrequest.md)                 | :heavy_check_mark:                                                  | The request object to use for the request.                          |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

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