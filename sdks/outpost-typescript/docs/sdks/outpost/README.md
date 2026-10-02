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

<!-- UsageSnippet language="typescript" operationID="publishEvent" method="post" path="/publish" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await outpost.publish({
    id: "evt_abc123xyz789",
    tenantId: "tenant_123",
    topic: "user.created",
    eligibleForRetry: true,
    metadata: {
      "source": "crm",
    },
    data: {
      "user_id": "userid",
      "status": "active",
    },
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { publish } from "@hookdeck/outpost-sdk/funcs/publish.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const res = await publish(outpost, {
    id: "evt_abc123xyz789",
    tenantId: "tenant_123",
    topic: "user.created",
    eligibleForRetry: true,
    metadata: {
      "source": "crm",
    },
    data: {
      "user_id": "userid",
      "status": "active",
    },
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("publish failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [components.PublishRequest](../../models/components/publishrequest.md)                                                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.PublishResponse](../../models/components/publishresponse.md)\>**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.NotFoundError         | 404                          | application/json             |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutError          | 408                          | application/json             |
| errors.APIErrorResponse      | 409                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 413, 414, 415, 422, 431      | application/json             |
| errors.TimeoutError          | 504                          | application/json             |
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

<!-- UsageSnippet language="typescript" operationID="retryEvent" method="post" path="/retry" example="RetryAccepted" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await outpost.retry({
    eventId: "evt_123",
    destinationId: "des_456",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { retry } from "@hookdeck/outpost-sdk/funcs/retry.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const res = await retry(outpost, {
    eventId: "evt_123",
    destinationId: "des_456",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("retry failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [components.RetryRequest](../../models/components/retryrequest.md)                                                                                                             | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.SuccessResponse](../../models/components/successresponse.md)\>**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.NotFoundError       | 404                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |