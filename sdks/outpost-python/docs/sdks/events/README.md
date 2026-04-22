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
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.events.list(request={})

    while res is not None:
        # Handle items

        res = res.next()

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `request`                                                           | [models.ListEventsRequest](../../models/listeventsrequest.md)       | :heavy_check_mark:                                                  | The request object to use for the request.                          |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

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

| Parameter                                                                                                                            | Type                                                                                                                                 | Required                                                                                                                             | Description                                                                                                                          |
| ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ |
| `event_id`                                                                                                                           | *str*                                                                                                                                | :heavy_check_mark:                                                                                                                   | The ID of the event.                                                                                                                 |
| `tenant_id`                                                                                                                          | *Optional[str]*                                                                                                                      | :heavy_minus_sign:                                                                                                                   | Filter by tenant ID. Returns 404 if the event does not belong to the specified tenant. Ignored when using Tenant JWT authentication. |
| `retries`                                                                                                                            | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                                                     | :heavy_minus_sign:                                                                                                                   | Configuration to override the default retry behavior of the client.                                                                  |

### Response

**[models.Event](../../models/event.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |