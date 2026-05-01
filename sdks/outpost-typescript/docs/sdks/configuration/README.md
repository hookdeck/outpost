# Configuration

## Overview

The Configuration API is available for **managed Outpost** deployments only. It allows you to read and update instance-level settings — the same settings available as environment variables in self-hosted deployments.


### Available Operations

* [getManagedConfig](#getmanagedconfig) - Get Managed Configuration
* [updateManagedConfig](#updatemanagedconfig) - Update Managed Configuration

## getManagedConfig

Returns managed Outpost configuration values.

This endpoint is only available for the managed version.
In self-hosted deployments, configuration is controlled through environment variables instead.


### Example Usage

<!-- UsageSnippet language="typescript" operationID="getManagedConfig" method="get" path="/config" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await outpost.configuration.getManagedConfig();

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { configurationGetManagedConfig } from "@hookdeck/outpost-sdk/funcs/configurationGetManagedConfig.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const res = await configurationGetManagedConfig(outpost);
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("configurationGetManagedConfig failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ManagedConfig](../../models/components/managedconfig.md)\>**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## updateManagedConfig

Updates one or more managed Outpost configuration values. Null values clear the configuration and reverts to Outpost default behavior.

This endpoint is only available for the managed version.
In self-hosted deployments, configuration is controlled through environment variables instead.

Only the supported configuration keys are accepted.


### Example Usage

<!-- UsageSnippet language="typescript" operationID="updateManagedConfig" method="patch" path="/config" -->
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await outpost.configuration.updateManagedConfig({
    destinationsWebhookMode: "default",
    topics: "user.created,user.updated",
  });

  console.log(result);
}

run();
```

### Standalone function

The standalone function version of this method:

```typescript
import { OutpostCore } from "@hookdeck/outpost-sdk/core.js";
import { configurationUpdateManagedConfig } from "@hookdeck/outpost-sdk/funcs/configurationUpdateManagedConfig.js";

// Use `OutpostCore` for best tree-shaking performance.
// You can create one instance of it to use across an application.
const outpost = new OutpostCore({
  apiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const res = await configurationUpdateManagedConfig(outpost, {
    destinationsWebhookMode: "default",
    topics: "user.created,user.updated",
  });
  if (res.ok) {
    const { value: result } = res;
    console.log(result);
  } else {
    console.log("configurationUpdateManagedConfig failed:", res.error);
  }
}

run();
```

### Parameters

| Parameter                                                                                                                                                                      | Type                                                                                                                                                                           | Required                                                                                                                                                                       | Description                                                                                                                                                                    |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `request`                                                                                                                                                                      | [components.ManagedConfig](../../models/components/managedconfig.md)                                                                                                           | :heavy_check_mark:                                                                                                                                                             | The request object to use for the request.                                                                                                                                     |
| `options`                                                                                                                                                                      | RequestOptions                                                                                                                                                                 | :heavy_minus_sign:                                                                                                                                                             | Used to set various options for making HTTP requests.                                                                                                                          |
| `options.fetchOptions`                                                                                                                                                         | [RequestInit](https://developer.mozilla.org/en-US/docs/Web/API/Request/Request#options)                                                                                        | :heavy_minus_sign:                                                                                                                                                             | Options that are passed to the underlying HTTP request. This can be used to inject extra headers for examples. All `Request` options, except `method` and `body`, are allowed. |
| `options.retries`                                                                                                                                                              | [RetryConfig](../../lib/utils/retryconfig.md)                                                                                                                                  | :heavy_minus_sign:                                                                                                                                                             | Enables retrying HTTP requests under certain failure conditions.                                                                                                               |

### Response

**Promise\<[components.ManagedConfig](../../models/components/managedconfig.md)\>**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |