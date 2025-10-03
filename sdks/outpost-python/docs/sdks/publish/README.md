# Publish
(*publish*)

## Overview

Operations for publishing events.

### Available Operations

* [event](#event) - Publish Event

## event

Publishes an event to the specified topic, potentially routed to a specific destination. Requires Admin API Key.

### Example Usage

<!-- UsageSnippet language="python" operationID="publishEvent" method="post" path="/publish" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.publish.event(data={
        "user_id": "userid",
        "status": "active",
    }, id="evt_custom_123", tenant_id="<TENANT_ID>", destination_id="<DESTINATION_ID>", topic="topic.name", metadata={
        "source": "crm",
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                               | Type                                                                                    | Required                                                                                | Description                                                                             | Example                                                                                 |
| --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| `data`                                                                                  | Dict[str, *Any*]                                                                        | :heavy_check_mark:                                                                      | Any JSON payload for the event data.                                                    | {<br/>"user_id": "userid",<br/>"status": "active"<br/>}                                 |
| `id`                                                                                    | *Optional[str]*                                                                         | :heavy_minus_sign:                                                                      | Optional. A unique identifier for the event. If not provided, a UUID will be generated. | evt_custom_123                                                                          |
| `tenant_id`                                                                             | *Optional[str]*                                                                         | :heavy_minus_sign:                                                                      | The ID of the tenant to publish for.                                                    | <TENANT_ID>                                                                             |
| `destination_id`                                                                        | *Optional[str]*                                                                         | :heavy_minus_sign:                                                                      | Optional. Route event to a specific destination.                                        | <DESTINATION_ID>                                                                        |
| `topic`                                                                                 | *Optional[str]*                                                                         | :heavy_minus_sign:                                                                      | Topic name for the event. Required if Outpost has been configured with topics.          | topic.name                                                                              |
| `eligible_for_retry`                                                                    | *Optional[bool]*                                                                        | :heavy_minus_sign:                                                                      | Should event delivery be retried on failure.                                            |                                                                                         |
| `metadata`                                                                              | Dict[str, *str*]                                                                        | :heavy_minus_sign:                                                                      | Any key-value string pairs for metadata.                                                | {<br/>"source": "crm"<br/>}                                                             |
| `retries`                                                                               | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                        | :heavy_minus_sign:                                                                      | Configuration to override the default retry behavior of the client.                     |                                                                                         |

### Response

**[models.PublishResponse](../../models/publishresponse.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.NotFoundError         | 404                          | application/json             |
| errors.UnauthorizedError     | 403, 407                     | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 413, 414, 415, 422, 431      | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |