# Outpost TypeScript SDK

Developer-friendly & type-safe Typescript SDK specifically catered to leverage the Outpost API.

<div align="left">
    <a href="https://www.speakeasy.com/?utm_source=outpost-github&utm_campaign=typescript"><img src="https://custom-icon-badges.demolab.com/badge/-Built%20By%20Speakeasy-212015?style=for-the-badge&logoColor=FBE331&logo=speakeasy&labelColor=545454" /></a>
    <a href="https://opensource.org/licenses/MIT">
        <img src="https://img.shields.io/badge/License-MIT-blue.svg" style="width: 100px; height: 28px;" />
    </a>
</div>

<!-- Start Summary [summary] -->
## Summary

Outpost API: The Outpost API is a REST-based JSON API for managing tenants, destinations, and publishing events.
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [Outpost TypeScript SDK](#outpost-typescript-sdk)
  * [SDK Installation](#sdk-installation)
  * [Requirements](#requirements)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [Standalone functions](#standalone-functions)
  * [Global Parameters](#global-parameters)
  * [Pagination](#pagination)
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
npm add @hookdeck/outpost-sdk
```

### PNPM

```bash
pnpm add @hookdeck/outpost-sdk
```

### Bun

```bash
bun add @hookdeck/outpost-sdk
```

### Yarn

```bash
yarn add @hookdeck/outpost-sdk zod

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
    "Outpost": {
      "command": "npx",
      "args": [
        "-y", "--package", "@hookdeck/outpost-sdk",
        "--",
        "mcp", "start",
        "--admin-api-key", "...",
        "--tenant-jwt", "...",
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
    "Outpost": {
      "command": "npx",
      "args": [
        "-y", "--package", "@hookdeck/outpost-sdk",
        "--",
        "mcp", "start",
        "--admin-api-key", "...",
        "--tenant-jwt", "...",
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
npx -y --package @hookdeck/outpost-sdk -- mcp start --help
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
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost();

async function run() {
  const result = await outpost.health.check();

  console.log(result);
}

run();

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security schemes globally:

| Name          | Type | Scheme      |
| ------------- | ---- | ----------- |
| `adminApiKey` | http | HTTP Bearer |
| `tenantJwt`   | http | HTTP Bearer |

You can set the security parameters through the `security` optional parameter when initializing the SDK client instance. The selected scheme will be used by default to authenticate with the API for all operations that support it. For example:
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.health.check();

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
* [getDestinationTypeJwt](docs/sdks/schemas/README.md#getdestinationtypejwt) - Get Destination Type Schema

### [tenants](docs/sdks/tenants/README.md)

* [upsert](docs/sdks/tenants/README.md#upsert) - Create or Update Tenant
* [get](docs/sdks/tenants/README.md#get) - Get Tenant
* [delete](docs/sdks/tenants/README.md#delete) - Delete Tenant
* [getPortalUrl](docs/sdks/tenants/README.md#getportalurl) - Get Portal Redirect URL
* [getToken](docs/sdks/tenants/README.md#gettoken) - Get Tenant JWT Token

### [topics](docs/sdks/topics/README.md)

* [list](docs/sdks/topics/README.md#list) - List Available Topics (for Tenant)
* [listJwt](docs/sdks/topics/README.md#listjwt) - List Available Topics)

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
- [`schemasGetDestinationTypeJwt`](docs/sdks/schemas/README.md#getdestinationtypejwt) - Get Destination Type Schema
- [`schemasListDestinationTypesJwt`](docs/sdks/schemas/README.md#listdestinationtypesjwt) - List Destination Type Schemas (JWT Auth)
- [`schemasListTenantDestinationTypes`](docs/sdks/schemas/README.md#listtenantdestinationtypes) - List Destination Type Schemas (for Tenant)
- [`tenantsDelete`](docs/sdks/tenants/README.md#delete) - Delete Tenant
- [`tenantsGet`](docs/sdks/tenants/README.md#get) - Get Tenant
- [`tenantsGetPortalUrl`](docs/sdks/tenants/README.md#getportalurl) - Get Portal Redirect URL
- [`tenantsGetToken`](docs/sdks/tenants/README.md#gettoken) - Get Tenant JWT Token
- [`tenantsUpsert`](docs/sdks/tenants/README.md#upsert) - Create or Update Tenant
- [`topicsList`](docs/sdks/topics/README.md#list) - List Available Topics (for Tenant)
- [`topicsListJwt`](docs/sdks/topics/README.md#listjwt) - List Available Topics)

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
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  tenantId: "<id>",
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.tenants.upsert({});

  console.log(result);
}

run();

```
<!-- End Global Parameters [global-parameters] -->

<!-- Start Pagination [pagination] -->
## Pagination

Some of the endpoints in this SDK support pagination. To use pagination, you
make your SDK calls as usual, but the returned response object will also be an
async iterable that can be consumed using the [`for await...of`][for-await-of]
syntax.

[for-await-of]: https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Statements/for-await...of

Here's an example of one such pagination call:

```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  tenantId: "<id>",
  security: {
    adminApiKey: "<YOUR_BEARER_TOKEN_HERE>",
  },
});

async function run() {
  const result = await outpost.events.list({});

  for await (const page of result) {
    console.log(page);
  }
}

run();

```
<!-- End Pagination [pagination] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries.  If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API.  However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a retryConfig object to the call:
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost();

async function run() {
  const result = await outpost.health.check({
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

  console.log(result);
}

run();

```

If you'd like to override the default retry strategy for all operations that support retries, you can provide a retryConfig at SDK initialization:
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
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
  const result = await outpost.health.check();

  console.log(result);
}

run();

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

[`OutpostError`](./src/models/errors/outposterror.ts) is the base class for all HTTP error responses. It has the following properties:

| Property            | Type       | Description                                                                             |
| ------------------- | ---------- | --------------------------------------------------------------------------------------- |
| `error.message`     | `string`   | Error message                                                                           |
| `error.statusCode`  | `number`   | HTTP response status code eg `404`                                                      |
| `error.headers`     | `Headers`  | HTTP response headers                                                                   |
| `error.body`        | `string`   | HTTP body. Can be empty string if no body is returned.                                  |
| `error.rawResponse` | `Response` | Raw HTTP response                                                                       |
| `error.data$`       |            | Optional. Some errors may contain structured data. [See Error Classes](#error-classes). |

### Example
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";
import * as errors from "@hookdeck/outpost-sdk/models/errors";

const outpost = new Outpost();

async function run() {
  try {
    const result = await outpost.health.check();

    console.log(result);
  } catch (error) {
    // The base class for HTTP error responses
    if (error instanceof errors.OutpostError) {
      console.log(error.message);
      console.log(error.statusCode);
      console.log(error.body);
      console.log(error.headers);

      // Depending on the method different errors may be thrown
      if (error instanceof errors.NotFoundError) {
        console.log(error.data$.message); // string
        console.log(error.data$.additionalProperties); // { [k: string]: any }
      }
    }
  }
}

run();

```

### Error Classes
**Primary errors:**
* [`OutpostError`](./src/models/errors/outposterror.ts): The base class for HTTP error responses.
  * [`BadRequestError`](./src/models/errors/badrequesterror.ts): A collection of codes that generally means the end user got something wrong in making the request.
  * [`UnauthorizedError`](./src/models/errors/unauthorizederror.ts): A collection of codes that generally means the client was not authenticated correctly for the request they want to make.
  * [`NotFoundError`](./src/models/errors/notfounderror.ts): Status codes relating to the resource/entity they are requesting not being found or endpoints/routes not existing.
  * [`TimeoutError`](./src/models/errors/timeouterror.ts): Timeouts occurred with the request.
  * [`RateLimitedError`](./src/models/errors/ratelimitederror.ts): Status codes relating to the client being rate limited by the server. Status code `429`.
  * [`InternalServerError`](./src/models/errors/internalservererror.ts): A collection of status codes that generally mean the server failed in an unexpected way.

<details><summary>Less common errors (6)</summary>

<br />

**Network errors:**
* [`ConnectionError`](./src/models/errors/httpclienterrors.ts): HTTP client was unable to make a request to a server.
* [`RequestTimeoutError`](./src/models/errors/httpclienterrors.ts): HTTP request timed out due to an AbortSignal signal.
* [`RequestAbortedError`](./src/models/errors/httpclienterrors.ts): HTTP request was aborted by the client.
* [`InvalidRequestError`](./src/models/errors/httpclienterrors.ts): Any input used to create a request is invalid.
* [`UnexpectedClientError`](./src/models/errors/httpclienterrors.ts): Unrecognised or unexpected error.


**Inherit from [`OutpostError`](./src/models/errors/outposterror.ts)**:
* [`ResponseValidationError`](./src/models/errors/responsevalidationerror.ts): Type mismatch between the data returned from the server and the structure expected by the SDK. See `error.rawValue` for the raw value and `error.pretty()` for a nicely formatted multi-line string.

</details>
<!-- End Error Handling [errors] -->

<!-- Start Server Selection [server] -->
## Server Selection

### Override Server URL Per-Client

The default server can be overridden globally by passing a URL to the `serverURL: string` optional parameter when initializing the SDK client instance. For example:
```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const outpost = new Outpost({
  serverURL: "http://localhost:3333/api/v1",
});

async function run() {
  const result = await outpost.health.check();

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
import { Outpost } from "@hookdeck/outpost-sdk";
import { HTTPClient } from "@hookdeck/outpost-sdk/lib/http";

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

const sdk = new Outpost({ httpClient });
```
<!-- End Custom HTTP Client [http-client] -->

<!-- Start Debugging [debug] -->
## Debugging

You can setup your SDK to emit debug logs for SDK requests and responses.

You can pass a logger that matches `console`'s interface as an SDK option.

> [!WARNING]
> Beware that debug logging will reveal secrets, like API tokens in headers, in log messages printed to a console or files. It's recommended to use this feature only during local development and not in production.

```typescript
import { Outpost } from "@hookdeck/outpost-sdk";

const sdk = new Outpost({ debugLogger: console });
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
