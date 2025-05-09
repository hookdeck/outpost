# openapi

Developer-friendly & type-safe Typescript SDK specifically catered to leverage *openapi* API.

<div align="left">
    <a href="https://www.speakeasy.com/?utm_source=openapi&utm_campaign=typescript"><img src="https://custom-icon-badges.demolab.com/badge/-Built%20By%20Speakeasy-212015?style=for-the-badge&logoColor=FBE331&logo=speakeasy&labelColor=545454" /></a>
    <a href="https://opensource.org/licenses/MIT">
        <img src="https://img.shields.io/badge/License-MIT-blue.svg" style="width: 100px; height: 28px;" />
    </a>
</div>


<br /><br />
> [!IMPORTANT]
> This SDK is not yet ready for production use. To complete setup please follow the steps outlined in your [workspace](https://app.speakeasy.com/org/speakeasy-onboarding/onboarding). Delete this section before > publishing to a package manager.

<!-- Start Summary [summary] -->
## Summary

Outpost API: The Outpost API is a REST-based JSON API for managing tenants, destinations, and publishing events.
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [openapi](#openapi)
  * [SDK Installation](#sdk-installation)
  * [Requirements](#requirements)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [Standalone functions](#standalone-functions)
  * [Global Parameters](#global-parameters)
  * [Retries](#retries)
  * [Error Handling](#error-handling)
  * [Server Selection](#server-selection)
  * [Custom HTTP Client](#custom-http-client)
  * [Debugging](#debugging)
* [Development](#development)
  * [Maturity](#maturity)
  * [Contributions](#contributions)

<!-- End Table of Contents [toc] -->

<!-- Start SDK Installation [installation] -->
## SDK Installation

The SDK can be installed with either [npm](https://www.npmjs.com/), [pnpm](https://pnpm.io/), [bun](https://bun.sh/) or [yarn](https://classic.yarnpkg.com/en/) package managers.

### NPM

```bash
npm add openapi
```

### PNPM

```bash
pnpm add openapi
```

### Bun

```bash
bun add openapi
```

### Yarn

```bash
yarn add openapi zod

# Note that Yarn does not install peer dependencies automatically. You will need
# to install zod as shown above.
```

> [!NOTE]
> This package is published with CommonJS and ES Modules (ESM) support.


### Model Context Protocol (MCP) Server

This SDK is also an installable MCP server where the various SDK methods are
exposed as tools that can be invoked by AI applications.

> Node.js v20 or greater is required to run the MCP server from npm.

<details>
<summary>Claude installation steps</summary>

Add the following server definition to your `claude_desktop_config.json` file:

```json
{
  "mcpServers": {
    "SDK": {
      "command": "npx",
      "args": [
        "-y", "--package", "openapi",
        "--",
        "mcp", "start",
        "--admin-api-key", "...",
        "--tenant-id", "..."
      ]
    }
  }
}
```

</details>

<details>
<summary>Cursor installation steps</summary>

Create a `.cursor/mcp.json` file in your project root with the following content:

```json
{
  "mcpServers": {
    "SDK": {
      "command": "npx",
      "args": [
        "-y", "--package", "openapi",
        "--",
        "mcp", "start",
        "--admin-api-key", "...",
        "--tenant-id", "..."
      ]
    }
  }
}
```

</details>

You can also run MCP servers as a standalone binary with no additional dependencies. You must pull these binaries from available Github releases:

```bash
curl -L -o mcp-server \
    https://github.com/{org}/{repo}/releases/download/{tag}/mcp-server-bun-darwin-arm64 && \
chmod +x mcp-server
```

If the repo is a private repo you must add your Github PAT to download a release `-H "Authorization: Bearer {GITHUB_PAT}"`.


```json
{
  "mcpServers": {
    "Todos": {
      "command": "./DOWNLOAD/PATH/mcp-server",
      "args": [
        "start"
      ]
    }
  }
}
```

For a full list of server arguments, run:

```sh
npx -y --package openapi -- mcp start --help
```
<!-- End SDK Installation [installation] -->

<!-- Start Requirements [requirements] -->
## Requirements

For supported JavaScript runtimes, please consult [RUNTIMES.md](RUNTIMES.md).
<!-- End Requirements [requirements] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```typescript
import { SDK } from "openapi";

const sdk = new SDK();

async function run() {
  const result = await sdk.health.check();

  // Handle the result
  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security scheme globally:

| Name          | Type | Scheme      |
| ------------- | ---- | ----------- |
| `adminApiKey` | http | HTTP Bearer |

To authenticate with the API the `adminApiKey` parameter must be set when initializing the SDK client instance. For example:
```typescript
import { SDK } from "openapi";

const sdk = new SDK({
  adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await sdk.health.check();

  // Handle the result
  console.log(result);
}

run();

```

### Per-Operation Security Schemes

Some operations in this SDK require the security scheme to be specified at the request level. For example:
```typescript
import { SDK } from "openapi";

const sdk = new SDK();

async function run() {
  const result = await sdk.tenants.get({}, {
    tenantId: "<id>",
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Authentication [security] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [destinations](docs/sdks/destinations/README.md)

* [list](docs/sdks/destinations/README.md#list) - List Destinations
* [create](docs/sdks/destinations/README.md#create) - Create Destination
* [get](docs/sdks/destinations/README.md#get) - Get Destination
* [update](docs/sdks/destinations/README.md#update) - Update Destination
* [delete](docs/sdks/destinations/README.md#delete) - Delete Destination
* [enable](docs/sdks/destinations/README.md#enable) - Enable Destination
* [disable](docs/sdks/destinations/README.md#disable) - Disable Destination

### [events](docs/sdks/events/README.md)

* [list](docs/sdks/events/README.md#list) - List Events
* [get](docs/sdks/events/README.md#get) - Get Event
* [listDeliveries](docs/sdks/events/README.md#listdeliveries) - List Event Delivery Attempts
* [listByDestination](docs/sdks/events/README.md#listbydestination) - List Events by Destination
* [getByDestination](docs/sdks/events/README.md#getbydestination) - Get Event by Destination
* [retry](docs/sdks/events/README.md#retry) - Retry Event Delivery

### [health](docs/sdks/health/README.md)

* [check](docs/sdks/health/README.md#check) - Health Check

### [publish](docs/sdks/publish/README.md)

* [event](docs/sdks/publish/README.md#event) - Publish Event

### [schemas](docs/sdks/schemas/README.md)

* [listTenantDestinationTypes](docs/sdks/schemas/README.md#listtenantdestinationtypes) - List Destination Type Schemas (for Tenant)
* [get](docs/sdks/schemas/README.md#get) - Get Destination Type Schema (for Tenant)
* [listDestinationTypesJwt](docs/sdks/schemas/README.md#listdestinationtypesjwt) - List Destination Type Schemas (JWT Auth)
* [getDestinationTypeJwt](docs/sdks/schemas/README.md#getdestinationtypejwt) - Get Destination Type Schema (JWT Auth)


### [tenants](docs/sdks/tenants/README.md)

* [upsert](docs/sdks/tenants/README.md#upsert) - Create or Update Tenant
* [get](docs/sdks/tenants/README.md#get) - Get Tenant
* [delete](docs/sdks/tenants/README.md#delete) - Delete Tenant
* [getPortalUrl](docs/sdks/tenants/README.md#getportalurl) - Get Portal Redirect URL
* [getToken](docs/sdks/tenants/README.md#gettoken) - Get Tenant JWT Token
* [getPortalUrlJwtContext](docs/sdks/tenants/README.md#getportalurljwtcontext) - Get Portal Redirect URL (JWT Auth Context)
* [getTokenJwtContext](docs/sdks/tenants/README.md#gettokenjwtcontext) - Get Tenant JWT Token (JWT Auth Context)

### [topics](docs/sdks/topics/README.md)

* [list](docs/sdks/topics/README.md#list) - List Available Topics (for Tenant)
* [listJwt](docs/sdks/topics/README.md#listjwt) - List Available Topics (JWT Auth)

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start Standalone functions [standalone-funcs] -->
## Standalone functions

All the methods listed above are available as standalone functions. These
functions are ideal for use in applications running in the browser, serverless
runtimes or other environments where application bundle size is a primary
concern. When using a bundler to build your application, all unused
functionality will be either excluded from the final bundle or tree-shaken away.

To read more about standalone functions, check [FUNCTIONS.md](./FUNCTIONS.md).

<details>

<summary>Available standalone functions</summary>

- [`destinationsCreate`](docs/sdks/destinations/README.md#create) - Create Destination
- [`destinationsDelete`](docs/sdks/destinations/README.md#delete) - Delete Destination
- [`destinationsDisable`](docs/sdks/destinations/README.md#disable) - Disable Destination
- [`destinationsEnable`](docs/sdks/destinations/README.md#enable) - Enable Destination
- [`destinationsGet`](docs/sdks/destinations/README.md#get) - Get Destination
- [`destinationsList`](docs/sdks/destinations/README.md#list) - List Destinations
- [`destinationsUpdate`](docs/sdks/destinations/README.md#update) - Update Destination
- [`eventsGet`](docs/sdks/events/README.md#get) - Get Event
- [`eventsGetByDestination`](docs/sdks/events/README.md#getbydestination) - Get Event by Destination
- [`eventsList`](docs/sdks/events/README.md#list) - List Events
- [`eventsListByDestination`](docs/sdks/events/README.md#listbydestination) - List Events by Destination
- [`eventsListDeliveries`](docs/sdks/events/README.md#listdeliveries) - List Event Delivery Attempts
- [`eventsRetry`](docs/sdks/events/README.md#retry) - Retry Event Delivery
- [`healthCheck`](docs/sdks/health/README.md#check) - Health Check
- [`publishEvent`](docs/sdks/publish/README.md#event) - Publish Event
- [`schemasGet`](docs/sdks/schemas/README.md#get) - Get Destination Type Schema (for Tenant)
- [`schemasGetDestinationTypeJwt`](docs/sdks/schemas/README.md#getdestinationtypejwt) - Get Destination Type Schema (JWT Auth)
- [`schemasListDestinationTypesJwt`](docs/sdks/schemas/README.md#listdestinationtypesjwt) - List Destination Type Schemas (JWT Auth)
- [`schemasListTenantDestinationTypes`](docs/sdks/schemas/README.md#listtenantdestinationtypes) - List Destination Type Schemas (for Tenant)
- [`tenantsDelete`](docs/sdks/tenants/README.md#delete) - Delete Tenant
- [`tenantsGet`](docs/sdks/tenants/README.md#get) - Get Tenant
- [`tenantsGetPortalUrl`](docs/sdks/tenants/README.md#getportalurl) - Get Portal Redirect URL
- [`tenantsGetPortalUrlJwtContext`](docs/sdks/tenants/README.md#getportalurljwtcontext) - Get Portal Redirect URL (JWT Auth Context)
- [`tenantsGetToken`](docs/sdks/tenants/README.md#gettoken) - Get Tenant JWT Token
- [`tenantsGetTokenJwtContext`](docs/sdks/tenants/README.md#gettokenjwtcontext) - Get Tenant JWT Token (JWT Auth Context)
- [`tenantsUpsert`](docs/sdks/tenants/README.md#upsert) - Create or Update Tenant
- [`topicsList`](docs/sdks/topics/README.md#list) - List Available Topics (for Tenant)
- [`topicsListJwt`](docs/sdks/topics/README.md#listjwt) - List Available Topics (JWT Auth)

</details>
<!-- End Standalone functions [standalone-funcs] -->

<!-- Start Global Parameters [global-parameters] -->
## Global Parameters

A parameter is configured globally. This parameter may be set on the SDK client instance itself during initialization. When configured as an option during SDK initialization, This global value will be used as the default on the operations that use it. When such operations are called, there is a place in each to override the global value, if needed.

For example, you can set `tenant_id` to `"<id>"` at SDK initialization and then you do not have to pass the same value on calls to operations like `upsert`. But if you want to do so you may, which will locally override the global setting. See the example code below for a demonstration.


### Available Globals

The following global parameter is available.

| Name     | Type   | Description             |
| -------- | ------ | ----------------------- |
| tenantId | string | The tenantId parameter. |

### Example

```typescript
import { SDK } from "openapi";

const sdk = new SDK({
  adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
});

async function run() {
  const result = await sdk.tenants.upsert({
    tenantId: "<id>",
  });

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Global Parameters [global-parameters] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries.  If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API.  However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a retryConfig object to the call:
```typescript
import { SDK } from "openapi";

const sdk = new SDK();

async function run() {
  const result = await sdk.health.check({
    retries: {
      strategy: "backoff",
      backoff: {
        initialInterval: 1,
        maxInterval: 50,
        exponent: 1.1,
        maxElapsedTime: 100,
      },
      retryConnectionErrors: false,
    },
  });

  // Handle the result
  console.log(result);
}

run();

```

If you'd like to override the default retry strategy for all operations that support retries, you can provide a retryConfig at SDK initialization:
```typescript
import { SDK } from "openapi";

const sdk = new SDK({
  retryConfig: {
    strategy: "backoff",
    backoff: {
      initialInterval: 1,
      maxInterval: 50,
      exponent: 1.1,
      maxElapsedTime: 100,
    },
    retryConnectionErrors: false,
  },
});

async function run() {
  const result = await sdk.health.check();

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

Some methods specify known errors which can be thrown. All the known errors are enumerated in the `models/errors/errors.ts` module. The known errors for a method are documented under the *Errors* tables in SDK docs. For example, the `check` method may throw the following errors:

| Error Type                 | Status Code                  | Content Type     |
| -------------------------- | ---------------------------- | ---------------- |
| errors.NotFoundError       | 404                          | application/json |
| errors.UnauthorizedError   | 401, 403, 407                | application/json |
| errors.TimeoutError        | 408                          | application/json |
| errors.RateLimitedError    | 429                          | application/json |
| errors.BadRequestError     | 400, 413, 414, 415, 422, 431 | application/json |
| errors.TimeoutError        | 504                          | application/json |
| errors.NotFoundError       | 501, 505                     | application/json |
| errors.InternalServerError | 500, 502, 503, 506, 507, 508 | application/json |
| errors.BadRequestError     | 510                          | application/json |
| errors.UnauthorizedError   | 511                          | application/json |
| errors.APIError            | 4XX, 5XX                     | \*/\*            |

If the method throws an error and it is not captured by the known errors, it will default to throwing a `APIError`.

```typescript
import { SDK } from "openapi";
import {
  BadRequestError,
  InternalServerError,
  NotFoundError,
  RateLimitedError,
  SDKValidationError,
  TimeoutError,
  UnauthorizedError,
} from "openapi/models/errors";

const sdk = new SDK();

async function run() {
  let result;
  try {
    result = await sdk.health.check();

    // Handle the result
    console.log(result);
  } catch (err) {
    switch (true) {
      // The server response does not match the expected SDK schema
      case (err instanceof SDKValidationError): {
        // Pretty-print will provide a human-readable multi-line error message
        console.error(err.pretty());
        // Raw value may also be inspected
        console.error(err.rawValue);
        return;
      }
      case (err instanceof NotFoundError): {
        // Handle err.data$: NotFoundErrorData
        console.error(err);
        return;
      }
      case (err instanceof UnauthorizedError): {
        // Handle err.data$: UnauthorizedErrorData
        console.error(err);
        return;
      }
      case (err instanceof TimeoutError): {
        // Handle err.data$: TimeoutErrorData
        console.error(err);
        return;
      }
      case (err instanceof RateLimitedError): {
        // Handle err.data$: RateLimitedErrorData
        console.error(err);
        return;
      }
      case (err instanceof BadRequestError): {
        // Handle err.data$: BadRequestErrorData
        console.error(err);
        return;
      }
      case (err instanceof TimeoutError): {
        // Handle err.data$: TimeoutErrorData
        console.error(err);
        return;
      }
      case (err instanceof NotFoundError): {
        // Handle err.data$: NotFoundErrorData
        console.error(err);
        return;
      }
      case (err instanceof InternalServerError): {
        // Handle err.data$: InternalServerErrorData
        console.error(err);
        return;
      }
      case (err instanceof BadRequestError): {
        // Handle err.data$: BadRequestErrorData
        console.error(err);
        return;
      }
      case (err instanceof UnauthorizedError): {
        // Handle err.data$: UnauthorizedErrorData
        console.error(err);
        return;
      }
      default: {
        // Other errors such as network errors, see HTTPClientErrors for more details
        throw err;
      }
    }
  }
}

run();

```

Validation errors can also occur when either method arguments or data returned from the server do not match the expected format. The `SDKValidationError` that is thrown as a result will capture the raw value that failed validation in an attribute called `rawValue`. Additionally, a `pretty()` method is available on this error that can be used to log a nicely formatted multi-line string since validation errors can list many issues and the plain error string may be difficult read when debugging.

In some rare cases, the SDK can fail to get a response from the server or even make the request due to unexpected circumstances such as network conditions. These types of errors are captured in the `models/errors/httpclienterrors.ts` module:

| HTTP Client Error                                    | Description                                          |
| ---------------------------------------------------- | ---------------------------------------------------- |
| RequestAbortedError                                  | HTTP request was aborted by the client               |
| RequestTimeoutError                                  | HTTP request timed out due to an AbortSignal signal  |
| ConnectionError                                      | HTTP client was unable to make a request to a server |
| InvalidRequestError                                  | Any input used to create a request is invalid        |
| UnexpectedClientError                                | Unrecognised or unexpected error                     |
<!-- End Error Handling [errors] -->

<!-- Start Server Selection [server] -->
## Server Selection

### Override Server URL Per-Client

The default server can be overridden globally by passing a URL to the `serverURL: string` optional parameter when initializing the SDK client instance. For example:
```typescript
import { SDK } from "openapi";

const sdk = new SDK({
  serverURL: "http://localhost:3333/api/v1",
});

async function run() {
  const result = await sdk.health.check();

  // Handle the result
  console.log(result);
}

run();

```
<!-- End Server Selection [server] -->

<!-- Start Custom HTTP Client [http-client] -->
## Custom HTTP Client

The TypeScript SDK makes API calls using an `HTTPClient` that wraps the native
[Fetch API](https://developer.mozilla.org/en-US/docs/Web/API/Fetch_API). This
client is a thin wrapper around `fetch` and provides the ability to attach hooks
around the request lifecycle that can be used to modify the request or handle
errors and response.

The `HTTPClient` constructor takes an optional `fetcher` argument that can be
used to integrate a third-party HTTP client or when writing tests to mock out
the HTTP client and feed in fixtures.

The following example shows how to use the `"beforeRequest"` hook to to add a
custom header and a timeout to requests and how to use the `"requestError"` hook
to log errors:

```typescript
import { SDK } from "openapi";
import { HTTPClient } from "openapi/lib/http";

const httpClient = new HTTPClient({
  // fetcher takes a function that has the same signature as native `fetch`.
  fetcher: (request) => {
    return fetch(request);
  }
});

httpClient.addHook("beforeRequest", (request) => {
  const nextRequest = new Request(request, {
    signal: request.signal || AbortSignal.timeout(5000)
  });

  nextRequest.headers.set("x-custom-header", "custom value");

  return nextRequest;
});

httpClient.addHook("requestError", (error, request) => {
  console.group("Request Error");
  console.log("Reason:", `${error}`);
  console.log("Endpoint:", `${request.method} ${request.url}`);
  console.groupEnd();
});

const sdk = new SDK({ httpClient });
```
<!-- End Custom HTTP Client [http-client] -->

<!-- Start Debugging [debug] -->
## Debugging

You can setup your SDK to emit debug logs for SDK requests and responses.

You can pass a logger that matches `console`'s interface as an SDK option.

> [!WARNING]
> Beware that debug logging will reveal secrets, like API tokens in headers, in log messages printed to a console or files. It's recommended to use this feature only during local development and not in production.

```typescript
import { SDK } from "openapi";

const sdk = new SDK({ debugLogger: console });
```
<!-- End Debugging [debug] -->

<!-- Placeholder for Future Speakeasy SDK Sections -->

# Development

## Maturity

This SDK is in beta, and there may be breaking changes between versions without a major version update. Therefore, we recommend pinning usage
to a specific package version. This way, you can install the same version each time without breaking changes unless you are intentionally
looking for the latest version.

## Contributions

While we value open-source contributions to this SDK, this library is generated programmatically. Any manual changes added to internal files will be overwritten on the next generation. 
We look forward to hearing your feedback. Feel free to open a PR or an issue with a proof of concept and we'll do our best to include it in a future release. 

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=openapi&utm_campaign=typescript)
