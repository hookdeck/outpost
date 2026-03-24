# Attempts

## Overview

Attempts represent individual delivery attempts of events to destinations. The attempts API provides an attempt-centric view of event processing.

Each attempt contains:
- `id`: Unique attempt identifier
- `status`: success or failed
- `time`: Timestamp of the attempt
- `code`: HTTP status code or error code
- `attempt`: Attempt number (1 for first attempt, 2+ for retries)
- `event`: Associated event (ID or included object)
- `destination`: Destination ID

Use the `include` query parameter to include related data:
- `include=event`: Include event summary (id, topic, time, eligible_for_retry, metadata)
- `include=event.data`: Include full event with payload data
- `include=response_data`: Include response body and headers from the attempt
- `include=destination`: Include the full destination object with target information


### Available Operations

* [list](#list) - List Attempts
* [get](#get) - Get Attempt
* [retry](#retry) - Retry Event Delivery

## list

Retrieves a paginated list of attempts.

When authenticated with a Tenant JWT, returns only attempts belonging to that tenant.
When authenticated with Admin API Key, returns attempts across all tenants. Use `tenant_id` query parameter to filter by tenant.


### Example Usage: AdminAttemptsListExample

<!-- UsageSnippet language="python" operationID="listAttempts" method="get" path="/attempts" example="AdminAttemptsListExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.attempts.list(request={})

    while res is not None:
        # Handle items

        res = res.next()

```
### Example Usage: AdminAttemptsWithIncludeExample

<!-- UsageSnippet language="python" operationID="listAttempts" method="get" path="/attempts" example="AdminAttemptsWithIncludeExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.attempts.list(request={})

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `request`                                                           | [models.ListAttemptsRequest](../../models/listattemptsrequest.md)   | :heavy_check_mark:                                                  | The request object to use for the request.                          |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.ListAttemptsResponse](../../models/listattemptsresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get

Retrieves details for a specific attempt.

When authenticated with a Tenant JWT, only attempts belonging to that tenant can be accessed.
When authenticated with Admin API Key, attempts from any tenant can be accessed.


### Example Usage: AttemptExample

<!-- UsageSnippet language="python" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.attempts.get(attempt_id="<id>")

    # Handle response
    print(res)

```
### Example Usage: AttemptWithIncludeExample

<!-- UsageSnippet language="python" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptWithIncludeExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.attempts.get(attempt_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                                                                                                                                                                                                                                                                                                         | Type                                                                                                                                                                                                                                                                                                                                                                              | Required                                                                                                                                                                                                                                                                                                                                                                          | Description                                                                                                                                                                                                                                                                                                                                                                       |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `attempt_id`                                                                                                                                                                                                                                                                                                                                                                      | *str*                                                                                                                                                                                                                                                                                                                                                                             | :heavy_check_mark:                                                                                                                                                                                                                                                                                                                                                                | The ID of the attempt.                                                                                                                                                                                                                                                                                                                                                            |
| `include`                                                                                                                                                                                                                                                                                                                                                                         | [Optional[models.GetAttemptInclude]](../../models/getattemptinclude.md)                                                                                                                                                                                                                                                                                                           | :heavy_minus_sign:                                                                                                                                                                                                                                                                                                                                                                | Fields to include in the response. Use bracket notation for multiple values (e.g., `include[0]=event&include[1]=response_data`).<br/>- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/>- `destination`: Include the full destination object<br/> |
| `retries`                                                                                                                                                                                                                                                                                                                                                                         | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                                                                                                                                                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                                                                                                                                                                                                                                | Configuration to override the default retry behavior of the client.                                                                                                                                                                                                                                                                                                               |

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

Triggers a retry for delivering an event to a destination. The event must exist and the destination must be enabled and match the event's topic.

When authenticated with a Tenant JWT, only events belonging to that tenant can be retried.
When authenticated with Admin API Key, events from any tenant can be retried.


### Example Usage

<!-- UsageSnippet language="python" operationID="retryEvent" method="post" path="/retry" example="RetryAccepted" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.attempts.retry(request={
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
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |