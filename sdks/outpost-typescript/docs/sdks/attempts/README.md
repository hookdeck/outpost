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

<!-- UsageSnippet language="typescript" operationID="adminListAttempts" method="get" path="/attempts" example="AdminAttemptsListExample" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.attempts.list({});

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { attemptsList } from "@hookdeck/outpost-sdk/funcs/attemptsList.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const res = await attemptsList(outpost, {});
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("attemptsList failed:", res.error);
  }
}

run();
```
### Example Usage: AdminAttemptsWithIncludeExample

<!-- UsageSnippet language="typescript" operationID="adminListAttempts" method="get" path="/attempts" example="AdminAttemptsWithIncludeExample" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.attempts.list({});

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { attemptsList } from "@hookdeck/outpost-sdk/funcs/attemptsList.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const res = await attemptsList(outpost, {});
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("attemptsList failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.AdminListAttemptsRequest](../../models/operations/adminlistattemptsrequest.md)                                                                                     | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.AttemptPaginatedResult](../../models/components/attemptpaginatedresult.md)\>**

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

<!-- UsageSnippet language="typescript" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptExample" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.attempts.get({
    attemptId: "<id>",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { attemptsGet } from "@hookdeck/outpost-sdk/funcs/attemptsGet.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const res = await attemptsGet(outpost, {
    attemptId: "<id>",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("attemptsGet failed:", res.error);
  }
}

run();
```
### Example Usage: AttemptWithIncludeExample

<!-- UsageSnippet language="typescript" operationID="getAttempt" method="get" path="/attempts/{attempt_id}" example="AttemptWithIncludeExample" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.attempts.get({
    attemptId: "<id>",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { attemptsGet } from "@hookdeck/outpost-sdk/funcs/attemptsGet.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const res = await attemptsGet(outpost, {
    attemptId: "<id>",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("attemptsGet failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetAttemptRequest](../../models/operations/getattemptrequest.md)                                                                                                   | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Attempt](../../models/components/attempt.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## retry

Triggers a retry for delivering an event to a destination. The event must exist and the destination must be enabled and match the event's topic.

When authenticated with a Tenant JWT, only events belonging to that tenant can be retried.
When authenticated with Admin API Key, events from any tenant can be retried.


### Example Usage

<!-- UsageSnippet language="typescript" operationID="retryEvent" method="post" path="/retry" example="RetryAccepted" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.attempts.retry({
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
import { attemptsRetry } from "@hookdeck/outpost-sdk/funcs/attemptsRetry.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const res = await attemptsRetry(outpost, {
    eventId: "evt_123",
    destinationId: "des_456",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("attemptsRetry failed:", res.error);
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

| Error Type              | Status Code             | Content Type            |
| ----------------------- | ----------------------- | ----------------------- |
| errors.APIErrorResponse | 422                     | application/json        |
| errors.APIError         | 4XX, 5XX                | \*/\*                   |