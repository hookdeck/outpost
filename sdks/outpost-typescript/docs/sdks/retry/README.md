# Retry

## Overview

Triggers a retry for delivering an event to a destination. The event must exist and the destination must be enabled and match the event's topic.

### Available Operations

* [retry](#retry) - Retry Event Delivery

## retry

Triggers a retry for delivering an event to a destination. The event must exist and the destination must be enabled and match the event's topic.

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
  const result = await outpost.retry.retry({
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
import { retryRetry } from "@hookdeck/outpost-sdk/funcs/retryRetry.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const res = await retryRetry(outpost, {
    eventId: "evt_123",
    destinationId: "des_456",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("retryRetry failed:", res.error);
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
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |