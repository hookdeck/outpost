# Events
(*events*)

## Overview

Operations related to event history and deliveries.

### Available Operations

* [list](#list) - List Events
* [get](#get) - Get Event
* [list_deliveries](#list_deliveries) - List Event Delivery Attempts
* [list_by_destination](#list_by_destination) - List Events by Destination
* [get_by_destination](#get_by_destination) - Get Event by Destination
* [retry](#retry) - Retry Event Delivery

## list

Retrieves a list of events for the tenant, supporting cursor navigation (details TBD) and filtering.

### Example Usage

<!-- UsageSnippet language="python" operationID="listTenantEvents" method="get" path="/{tenant_id}/events" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.events.list(limit=100)

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                                         | Type                                                                              | Required                                                                          | Description                                                                       |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `tenant_id`                                                                       | *Optional[str]*                                                                   | :heavy_minus_sign:                                                                | The ID of the tenant. Required when using AdminApiKey authentication.             |
| `destination_id`                                                                  | [Optional[models.DestinationID]](../../models/destinationid.md)                   | :heavy_minus_sign:                                                                | Filter events by destination ID(s).                                               |
| `status`                                                                          | [Optional[models.ListTenantEventsStatus]](../../models/listtenanteventsstatus.md) | :heavy_minus_sign:                                                                | Filter events by delivery status.                                                 |
| `next`                                                                            | *Optional[str]*                                                                   | :heavy_minus_sign:                                                                | Cursor for next page of results                                                   |
| `prev`                                                                            | *Optional[str]*                                                                   | :heavy_minus_sign:                                                                | Cursor for previous page of results                                               |
| `limit`                                                                           | *Optional[int]*                                                                   | :heavy_minus_sign:                                                                | Number of items per page (default 100, max 1000)                                  |
| `start`                                                                           | [date](https://docs.python.org/3/library/datetime.html#date-objects)              | :heavy_minus_sign:                                                                | Start time filter (RFC3339 format)                                                |
| `end`                                                                             | [date](https://docs.python.org/3/library/datetime.html#date-objects)              | :heavy_minus_sign:                                                                | End time filter (RFC3339 format)                                                  |
| `retries`                                                                         | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                  | :heavy_minus_sign:                                                                | Configuration to override the default retry behavior of the client.               |

### Response

**[models.ListTenantEventsResponse](../../models/listtenanteventsresponse.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## get

Retrieves details for a specific event.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantEvent" method="get" path="/{tenant_id}/events/{event_id}" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.events.get(event_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `event_id`                                                            | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the event.                                                  |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Event](../../models/event.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## list_deliveries

Retrieves a list of delivery attempts for a specific event, including response details.

### Example Usage

<!-- UsageSnippet language="python" operationID="listTenantEventDeliveries" method="get" path="/{tenant_id}/events/{event_id}/deliveries" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.events.list_deliveries(event_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `event_id`                                                            | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the event.                                                  |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[List[models.DeliveryAttempt]](../../models/.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## list_by_destination

Retrieves events associated with a specific destination for the tenant.

### Example Usage

<!-- UsageSnippet language="python" operationID="listTenantEventsByDestination" method="get" path="/{tenant_id}/destinations/{destination_id}/events" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.events.list_by_destination(destination_id="<id>", limit=100)

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                                                                   | Type                                                                                                        | Required                                                                                                    | Description                                                                                                 |
| ----------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `destination_id`                                                                                            | *str*                                                                                                       | :heavy_check_mark:                                                                                          | The ID of the destination.                                                                                  |
| `tenant_id`                                                                                                 | *Optional[str]*                                                                                             | :heavy_minus_sign:                                                                                          | The ID of the tenant. Required when using AdminApiKey authentication.                                       |
| `status`                                                                                                    | [Optional[models.ListTenantEventsByDestinationStatus]](../../models/listtenanteventsbydestinationstatus.md) | :heavy_minus_sign:                                                                                          | Filter events by delivery status.                                                                           |
| `next`                                                                                                      | *Optional[str]*                                                                                             | :heavy_minus_sign:                                                                                          | Cursor for next page of results                                                                             |
| `prev`                                                                                                      | *Optional[str]*                                                                                             | :heavy_minus_sign:                                                                                          | Cursor for previous page of results                                                                         |
| `limit`                                                                                                     | *Optional[int]*                                                                                             | :heavy_minus_sign:                                                                                          | Number of items per page (default 100, max 1000)                                                            |
| `start`                                                                                                     | [date](https://docs.python.org/3/library/datetime.html#date-objects)                                        | :heavy_minus_sign:                                                                                          | Start time filter (RFC3339 format)                                                                          |
| `end`                                                                                                       | [date](https://docs.python.org/3/library/datetime.html#date-objects)                                        | :heavy_minus_sign:                                                                                          | End time filter (RFC3339 format)                                                                            |
| `retries`                                                                                                   | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                            | :heavy_minus_sign:                                                                                          | Configuration to override the default retry behavior of the client.                                         |

### Response

**[models.ListTenantEventsByDestinationResponse](../../models/listtenanteventsbydestinationresponse.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## get_by_destination

Retrieves a specific event associated with a specific destination for the tenant.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantEventByDestination" method="get" path="/{tenant_id}/destinations/{destination_id}/events/{event_id}" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.events.get_by_destination(destination_id="<id>", event_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `event_id`                                                            | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the event.                                                  |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Event](../../models/event.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## retry

Triggers a retry for a failed event delivery.

### Example Usage

<!-- UsageSnippet language="python" operationID="retryTenantEvent" method="post" path="/{tenant_id}/destinations/{destination_id}/events/{event_id}/retry" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    outpost.events.retry(destination_id="<id>", event_id="<id>")

    # Use the SDK ...

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `destination_id`                                                      | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the destination.                                            |
| `event_id`                                                            | *str*                                                                 | :heavy_check_mark:                                                    | The ID of the event to retry.                                         |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |