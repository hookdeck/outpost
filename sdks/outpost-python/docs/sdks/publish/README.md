# Publish

## Overview

Operations for publishing events.

### Available Operations

* [event](#event) - Publish Event

## event

Publishes an event to the specified topic, potentially routed to a specific destination. Requires Admin API Key.

### Example Usage

<!-- UsageSnippet language="python" operationID="publishEvent" method="post" path="/publish" -->
```python
from outpost_sdk import Outpost
from outpost_sdk.utils import parse_datetime


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.publish.event(request={
        "id": "evt_custom_123",
        "tenant_id": "<TENANT_ID>",
        "destination_id": "<DESTINATION_ID>",
        "topic": "topic.name",
        "time": parse_datetime("2024-01-15T10:30:00Z"),
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
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 413, 414, 415, 422, 431      | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |