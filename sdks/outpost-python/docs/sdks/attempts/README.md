# Attempts

## Overview

Attempts represent individual delivery attempts of events to destinations. The attempts API provides an attempt-centric view of event processing.

Use the `include` query parameter to include related data:
- `include=event`: Include event summary (id, topic, time, eligible_for_retry, metadata)
- `include=event.data`: Include full event with payload data
- `include=response_data`: Include response body and headers from the attempt
- `include=destination`: Include the full destination object with target information


### Available Operations

* [list](#list) - List Attempts
* [get](#get) - Get Attempt

## list

Retrieves a paginated list of attempts.

When authenticated with a Tenant JWT, returns only attempts belonging to that tenant.
When authenticated with Admin API Key, returns attempts across all tenants. Use `tenant_id` query parameter to filter by tenant.


### Example Usage: AdminAttemptsListExample

<!-- UsageSnippet language="python" operationID="listAttempts" method="get" path="/attempts" example="AdminAttemptsListExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.attempts.list(request={
        "destination_type": models.DestinationType.WEBHOOK,
    })

    while res is not None:
        # Handle items

        res = res.next()

```
### Example Usage: AdminAttemptsWithIncludeExample

<!-- UsageSnippet language="python" operationID="listAttempts" method="get" path="/attempts" example="AdminAttemptsWithIncludeExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.attempts.list(request={
        "destination_type": models.DestinationType.WEBHOOK,
    })

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
| `tenant_id`                                                                                                                                                                                                                                                                                                                                                                       | *Optional[str]*                                                                                                                                                                                                                                                                                                                                                                   | :heavy_minus_sign:                                                                                                                                                                                                                                                                                                                                                                | Filter by tenant ID. Returns 404 if the attempt does not belong to the specified tenant. Ignored when using Tenant JWT authentication.                                                                                                                                                                                                                                            |
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