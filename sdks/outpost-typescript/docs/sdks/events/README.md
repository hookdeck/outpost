# Events

## Overview

Operations related to event history.

### Available Operations

* [list](#list) - List Events (Admin)
* [get](#get) - Get Event

## list

Retrieves a list of events across all tenants. This is an admin-only endpoint that requires the Admin API Key.

When `tenant_id` is not provided, returns events from all tenants. When `tenant_id` is provided, returns only events for that tenant.


### Example Usage

<!-- UsageSnippet language="typescript" operationID="adminListEvents" method="get" path="/events" example="AdminEventsListExample" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.events.list({});

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { eventsList } from "@hookdeck/outpost-sdk/funcs/eventsList.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const res = await eventsList(outpost, {});
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("eventsList failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.AdminListEventsRequest](../../models/operations/adminlisteventsrequest.md)                                                                                         | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.EventPaginatedResult](../../models/components/eventpaginatedresult.md)\>**

### Errors

| Error Type              | Status Code             | Content Type            |
| ----------------------- | ----------------------- | ----------------------- |
| errors.APIErrorResponse | 422                     | application/json        |
| errors.APIError         | 4XX, 5XX                | \*/\*                   |

## get

Retrieves details for a specific event.

When authenticated with a Tenant JWT, only events belonging to that tenant can be accessed.
When authenticated with Admin API Key, events from any tenant can be accessed.


### Example Usage

<!-- UsageSnippet language="typescript" operationID="getEvent" method="get" path="/events/{event_id}" example="EventExample" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.events.get({
    eventId: "<id>",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { eventsGet } from "@hookdeck/outpost-sdk/funcs/eventsGet.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const res = await eventsGet(outpost, {
    eventId: "<id>",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("eventsGet failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [operations.GetEventRequest](../../models/operations/geteventrequest.md)                                                                                                       | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.Event](../../models/components/event.md)\>**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |