# Events

## Overview

Operations related to event history.

### Available Operations

* [list](#list) - List Events
* [get](#get) - Get Event

## list

Retrieves a list of events.

When authenticated with a Tenant JWT, returns only events belonging to that tenant.
When authenticated with Admin API Key, returns events across all tenants. Use `tenant_id` query parameter to filter by tenant.


### Example Usage

<!-- UsageSnippet language="python" operationID="listEvents" method="get" path="/events" example="AdminEventsListExample" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.events.list(limit=100, order_by=models.ListEventsOrderBy.TIME, direction=models.ListEventsDir.DESC)

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                                      | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `tenant_id`                                                                    | *Optional[str]*                                                                | :heavy_minus_sign:                                                             | Filter events by tenant ID. If not provided, returns events from all tenants.  |
| `topic`                                                                        | [Optional[models.ListEventsTopic]](../../models/listeventstopic.md)            | :heavy_minus_sign:                                                             | Filter events by topic(s). Can be specified multiple times or comma-separated. |
| `time_gte`                                                                     | [date](https://docs.python.org/3/library/datetime.html#date-objects)           | :heavy_minus_sign:                                                             | Filter events with time >= value (RFC3339 or YYYY-MM-DD format).               |
| `time_lte`                                                                     | [date](https://docs.python.org/3/library/datetime.html#date-objects)           | :heavy_minus_sign:                                                             | Filter events with time <= value (RFC3339 or YYYY-MM-DD format).               |
| `limit`                                                                        | *Optional[int]*                                                                | :heavy_minus_sign:                                                             | Number of items per page (default 100, max 1000).                              |
| `next_cursor`                                                                  | *Optional[str]*                                                                | :heavy_minus_sign:                                                             | Cursor for next page of results.                                               |
| `prev_cursor`                                                                  | *Optional[str]*                                                                | :heavy_minus_sign:                                                             | Cursor for previous page of results.                                           |
| `order_by`                                                                     | [Optional[models.ListEventsOrderBy]](../../models/listeventsorderby.md)        | :heavy_minus_sign:                                                             | Field to sort by.                                                              |
| `direction`                                                                    | [Optional[models.ListEventsDir]](../../models/listeventsdir.md)                | :heavy_minus_sign:                                                             | Sort direction.                                                                |
| `retries`                                                                      | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)               | :heavy_minus_sign:                                                             | Configuration to override the default retry behavior of the client.            |

### Response

**[models.ListEventsResponse](../../models/listeventsresponse.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## get

Retrieves details for a specific event.

When authenticated with a Tenant JWT, only events belonging to that tenant can be accessed.
When authenticated with Admin API Key, events from any tenant can be accessed.


### Example Usage

<!-- UsageSnippet language="python" operationID="getEvent" method="get" path="/events/{event_id}" example="EventExample" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.events.get(event_id="<id>")

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `event_id`                                                          | *str*                                                               | :heavy_check_mark:                                                  | The ID of the event.                                                |
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