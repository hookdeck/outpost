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


### Available Operations

* [list](#list) - List Attempts (Admin)
* [get](#get) - Get Attempt
* [retry](#retry) - Retry Event Delivery

## list

Retrieves a paginated list of attempts across all tenants. This is an admin-only endpoint that requires the Admin API Key.

When `tenant_id` is not provided, returns attempts from all tenants. When `tenant_id` is provided, returns only attempts for that tenant.


### Example Usage: AdminAttemptsListExample

<!-- UsageSnippet language="python" operationID="adminListAttempts" method="get" path="/attempts" example="AdminAttemptsListExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.attempts.list(limit=100, order_by=models.AdminListAttemptsOrderBy.TIME, direction=models.AdminListAttemptsDir.DESC)

    while res is not None:
        # Handle items

        res = res.next()

```
### Example Usage: AdminAttemptsWithIncludeExample

<!-- UsageSnippet language="python" operationID="adminListAttempts" method="get" path="/attempts" example="AdminAttemptsWithIncludeExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.attempts.list(limit=100, order_by=models.AdminListAttemptsOrderBy.TIME, direction=models.AdminListAttemptsDir.DESC)

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                                                                                                                                                                                                                                          | Type                                                                                                                                                                                                                                                                               | Required                                                                                                                                                                                                                                                                           | Description                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `tenant_id`                                                                                                                                                                                                                                                                        | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by tenant ID. If not provided, returns attempts from all tenants.                                                                                                                                                                                                  |
| `event_id`                                                                                                                                                                                                                                                                         | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event ID.                                                                                                                                                                                                                                                       |
| `destination_id`                                                                                                                                                                                                                                                                   | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by destination ID.                                                                                                                                                                                                                                                 |
| `status`                                                                                                                                                                                                                                                                           | [Optional[models.AdminListAttemptsStatus]](../../models/adminlistattemptsstatus.md)                                                                                                                                                                                                | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by status.                                                                                                                                                                                                                                                         |
| `topic`                                                                                                                                                                                                                                                                            | [Optional[models.AdminListAttemptsTopic]](../../models/adminlistattemptstopic.md)                                                                                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event topic(s). Can be specified multiple times or comma-separated.                                                                                                                                                                                             |
| `time_gte`                                                                                                                                                                                                                                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects)                                                                                                                                                                                                               | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event time >= value (RFC3339 or YYYY-MM-DD format).                                                                                                                                                                                                             |
| `time_lte`                                                                                                                                                                                                                                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects)                                                                                                                                                                                                               | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Filter attempts by event time <= value (RFC3339 or YYYY-MM-DD format).                                                                                                                                                                                                             |
| `limit`                                                                                                                                                                                                                                                                            | *Optional[int]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Number of items per page (default 100, max 1000).                                                                                                                                                                                                                                  |
| `next_cursor`                                                                                                                                                                                                                                                                      | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Cursor for next page of results.                                                                                                                                                                                                                                                   |
| `prev_cursor`                                                                                                                                                                                                                                                                      | *Optional[str]*                                                                                                                                                                                                                                                                    | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Cursor for previous page of results.                                                                                                                                                                                                                                               |
| `include`                                                                                                                                                                                                                                                                          | [Optional[models.AdminListAttemptsInclude]](../../models/adminlistattemptsinclude.md)                                                                                                                                                                                              | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Fields to include in the response. Can be specified multiple times or comma-separated.<br/>- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/> |
| `order_by`                                                                                                                                                                                                                                                                         | [Optional[models.AdminListAttemptsOrderBy]](../../models/adminlistattemptsorderby.md)                                                                                                                                                                                              | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Field to sort by.                                                                                                                                                                                                                                                                  |
| `direction`                                                                                                                                                                                                                                                                        | [Optional[models.AdminListAttemptsDir]](../../models/adminlistattemptsdir.md)                                                                                                                                                                                                      | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Sort direction.                                                                                                                                                                                                                                                                    |
| `retries`                                                                                                                                                                                                                                                                          | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                                                                                                                                                                   | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Configuration to override the default retry behavior of the client.                                                                                                                                                                                                                |

### Response

**[models.AdminListAttemptsResponse](../../models/adminlistattemptsresponse.md)**

### Errors

| Error Type              | Status Code             | Content Type            |
| ----------------------- | ----------------------- | ----------------------- |
| errors.APIErrorResponse | 422                     | application/json        |
| errors.APIError         | 4XX, 5XX                | \*/\*                   |

## get

Retrieves details for a specific attempt.

When authenticated with a Tenant JWT, only attempts belonging to that tenant can be accessed.
When authenticated with Admin API Key, attempts from any tenant can be accessed.


### Example Usage: AttemptExample

<!-- UsageSnippet language="python" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.attempts.get(attempt_id="<id>")

    # Handle response
    print(res)

```
### Example Usage: AttemptWithIncludeExample

<!-- UsageSnippet language="python" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptWithIncludeExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.attempts.get(attempt_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                                                                                                                                                                                                          | Type                                                                                                                                                                                                                                                                               | Required                                                                                                                                                                                                                                                                           | Description                                                                                                                                                                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `attempt_id`                                                                                                                                                                                                                                                                       | *str*                                                                                                                                                                                                                                                                              | :heavy_check_mark:                                                                                                                                                                                                                                                                 | The ID of the attempt.                                                                                                                                                                                                                                                             |
| `include`                                                                                                                                                                                                                                                                          | [Optional[models.GetAttemptInclude]](../../models/getattemptinclude.md)                                                                                                                                                                                                            | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Fields to include in the response. Can be specified multiple times or comma-separated.<br/>- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)<br/>- `event.data`: Include full event with payload data<br/>- `response_data`: Include response body and headers<br/> |
| `retries`                                                                                                                                                                                                                                                                          | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                                                                                                                                                                   | :heavy_minus_sign:                                                                                                                                                                                                                                                                 | Configuration to override the default retry behavior of the client.                                                                                                                                                                                                                |

### Response

**[models.Attempt](../../models/attempt.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## retry

Triggers a retry for delivering an event to a destination. The event must exist and the destination must be enabled and match the event's topic.

When authenticated with a Tenant JWT, only events belonging to that tenant can be retried.
When authenticated with Admin API Key, events from any tenant can be retried.


### Example Usage

<!-- UsageSnippet language="python" operationID="retryEvent" method="post" path="/retry" example="RetryAccepted" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.attempts.retry(event_id="evt_123", destination_id="des_456")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         | Example                                                             |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `event_id`                                                          | *str*                                                               | :heavy_check_mark:                                                  | The ID of the event to retry.                                       | evt_123                                                             |
| `destination_id`                                                    | *str*                                                               | :heavy_check_mark:                                                  | The ID of the destination to deliver to.                            | des_456                                                             |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |                                                                     |

### Response

**[models.SuccessResponse](../../models/successresponse.md)**

### Errors

| Error Type              | Status Code             | Content Type            |
| ----------------------- | ----------------------- | ----------------------- |
| errors.APIErrorResponse | 422                     | application/json        |
| errors.APIError         | 4XX, 5XX                | \*/\*                   |