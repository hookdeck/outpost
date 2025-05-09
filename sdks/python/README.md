# openapi

Developer-friendly & type-safe Python SDK specifically catered to leverage *openapi* API.

<div align="left">
    <a href="https://www.speakeasy.com/?utm_source=openapi&utm_campaign=python"><img src="https://custom-icon-badges.demolab.com/badge/-Built%20By%20Speakeasy-212015?style=for-the-badge&logoColor=FBE331&logo=speakeasy&labelColor=545454" /></a>
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
  * [IDE Support](#ide-support)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [Global Parameters](#global-parameters)
  * [Retries](#retries)
  * [Error Handling](#error-handling)
  * [Server Selection](#server-selection)
  * [Custom HTTP Client](#custom-http-client)
  * [Resource Management](#resource-management)
  * [Debugging](#debugging)
* [Development](#development)
  * [Maturity](#maturity)
  * [Contributions](#contributions)

<!-- End Table of Contents [toc] -->

<!-- Start SDK Installation [installation] -->
## SDK Installation

> [!NOTE]
> **Python version upgrade policy**
>
> Once a Python version reaches its [official end of life date](https://devguide.python.org/versions/), a 3-month grace period is provided for users to upgrade. Following this grace period, the minimum python version supported in the SDK will be updated.

The SDK can be installed with either *pip* or *poetry* package managers.

### PIP

*PIP* is the default package installer for Python, enabling easy installation and management of packages from PyPI via the command line.

```bash
pip install openapi
```

### Poetry

*Poetry* is a modern tool that simplifies dependency management and package publishing by using a single `pyproject.toml` file to handle project metadata and dependencies.

```bash
poetry add openapi
```

### Shell and script usage with `uv`

You can use this SDK in a Python shell with [uv](https://docs.astral.sh/uv/) and the `uvx` command that comes with it like so:

```shell
uvx --from openapi python
```

It's also possible to write a standalone Python script without needing to set up a whole project like so:

```python
#!/usr/bin/env -S uv run --script
# /// script
# requires-python = ">=3.9"
# dependencies = [
#     "openapi",
# ]
# ///

from openapi import SDK

sdk = SDK(
  # SDK arguments
)

# Rest of script here...
```

Once that is saved to a file, you can run it with `uv run script.py` where
`script.py` can be replaced with the actual file name.
<!-- End SDK Installation [installation] -->

<!-- Start IDE Support [idesupport] -->
## IDE Support

### PyCharm

Generally, the SDK will work well with most IDEs out of the box. However, when using PyCharm, you can enjoy much better integration with Pydantic by installing an additional plugin.

- [PyCharm Pydantic Plugin](https://docs.pydantic.dev/latest/integrations/pycharm/)
<!-- End IDE Support [idesupport] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```python
# Synchronous Example
from openapi import SDK


with SDK() as sdk:

    res = sdk.health.check()

    # Handle response
    print(res)
```

</br>

The same SDK client can also be used to make asychronous requests by importing asyncio.
```python
# Asynchronous Example
import asyncio
from openapi import SDK

async def main():

    async with SDK() as sdk:

        res = await sdk.health.check_async()

        # Handle response
        print(res)

asyncio.run(main())
```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security scheme globally:

| Name            | Type | Scheme      |
| --------------- | ---- | ----------- |
| `admin_api_key` | http | HTTP Bearer |

To authenticate with the API the `admin_api_key` parameter must be set when initializing the SDK client instance. For example:
```python
from openapi import SDK


with SDK(
    admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
) as sdk:

    res = sdk.health.check()

    # Handle response
    print(res)

```

### Per-Operation Security Schemes

Some operations in this SDK require the security scheme to be specified at the request level. For example:
```python
from openapi import SDK, models


with SDK() as sdk:

    res = sdk.tenants.get(security=models.GetTenantSecurity(

    ), tenant_id="<id>")

    # Handle response
    print(res)

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
* [list_deliveries](docs/sdks/events/README.md#list_deliveries) - List Event Delivery Attempts
* [list_by_destination](docs/sdks/events/README.md#list_by_destination) - List Events by Destination
* [get_by_destination](docs/sdks/events/README.md#get_by_destination) - Get Event by Destination
* [retry](docs/sdks/events/README.md#retry) - Retry Event Delivery

### [health](docs/sdks/health/README.md)

* [check](docs/sdks/health/README.md#check) - Health Check

### [publish](docs/sdks/publish/README.md)

* [event](docs/sdks/publish/README.md#event) - Publish Event

### [schemas](docs/sdks/schemas/README.md)

* [list_tenant_destination_types](docs/sdks/schemas/README.md#list_tenant_destination_types) - List Destination Type Schemas (for Tenant)
* [get](docs/sdks/schemas/README.md#get) - Get Destination Type Schema (for Tenant)
* [list_destination_types_jwt](docs/sdks/schemas/README.md#list_destination_types_jwt) - List Destination Type Schemas (JWT Auth)
* [get_destination_type_jwt](docs/sdks/schemas/README.md#get_destination_type_jwt) - Get Destination Type Schema (JWT Auth)


### [tenants](docs/sdks/tenants/README.md)

* [upsert](docs/sdks/tenants/README.md#upsert) - Create or Update Tenant
* [get](docs/sdks/tenants/README.md#get) - Get Tenant
* [delete](docs/sdks/tenants/README.md#delete) - Delete Tenant
* [get_portal_url](docs/sdks/tenants/README.md#get_portal_url) - Get Portal Redirect URL
* [get_token](docs/sdks/tenants/README.md#get_token) - Get Tenant JWT Token
* [get_portal_url_jwt_context](docs/sdks/tenants/README.md#get_portal_url_jwt_context) - Get Portal Redirect URL (JWT Auth Context)
* [get_token_jwt_context](docs/sdks/tenants/README.md#get_token_jwt_context) - Get Tenant JWT Token (JWT Auth Context)

### [topics](docs/sdks/topicssdk/README.md)

* [list](docs/sdks/topicssdk/README.md#list) - List Available Topics (for Tenant)
* [list_jwt](docs/sdks/topicssdk/README.md#list_jwt) - List Available Topics (JWT Auth)

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start Global Parameters [global-parameters] -->
## Global Parameters

A parameter is configured globally. This parameter may be set on the SDK client instance itself during initialization. When configured as an option during SDK initialization, This global value will be used as the default on the operations that use it. When such operations are called, there is a place in each to override the global value, if needed.

For example, you can set `tenant_id` to `"<id>"` at SDK initialization and then you do not have to pass the same value on calls to operations like `upsert`. But if you want to do so you may, which will locally override the global setting. See the example code below for a demonstration.


### Available Globals

The following global parameter is available.

| Name      | Type | Description              |
| --------- | ---- | ------------------------ |
| tenant_id | str  | The tenant_id parameter. |

### Example

```python
from openapi import SDK


with SDK(
    admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
) as sdk:

    res = sdk.tenants.upsert(tenant_id="<id>")

    # Handle response
    print(res)

```
<!-- End Global Parameters [global-parameters] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries. If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API. However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a `RetryConfig` object to the call:
```python
from openapi import SDK
from openapi.utils import BackoffStrategy, RetryConfig


with SDK() as sdk:

    res = sdk.health.check(,
        RetryConfig("backoff", BackoffStrategy(1, 50, 1.1, 100), False))

    # Handle response
    print(res)

```

If you'd like to override the default retry strategy for all operations that support retries, you can use the `retry_config` optional parameter when initializing the SDK:
```python
from openapi import SDK
from openapi.utils import BackoffStrategy, RetryConfig


with SDK(
    retry_config=RetryConfig("backoff", BackoffStrategy(1, 50, 1.1, 100), False),
) as sdk:

    res = sdk.health.check()

    # Handle response
    print(res)

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

Handling errors in this SDK should largely match your expectations. All operations return a response object or raise an exception.

By default, an API error will raise a errors.APIError exception, which has the following properties:

| Property        | Type             | Description           |
|-----------------|------------------|-----------------------|
| `.status_code`  | *int*            | The HTTP status code  |
| `.message`      | *str*            | The error message     |
| `.raw_response` | *httpx.Response* | The raw HTTP response |
| `.body`         | *str*            | The response content  |

When custom error responses are specified for an operation, the SDK may also raise their associated exceptions. You can refer to respective *Errors* tables in SDK docs for more details on possible exception types for each operation. For example, the `check_async` method may raise the following exceptions:

| Error Type                 | Status Code                  | Content Type     |
| -------------------------- | ---------------------------- | ---------------- |
| errors.NotFoundError       | 404                          | application/json |
| errors.UnauthorizedError   | 401, 403, 407                | application/json |
| errors.TimeoutErrorT       | 408                          | application/json |
| errors.RateLimitedError    | 429                          | application/json |
| errors.BadRequestError     | 400, 413, 414, 415, 422, 431 | application/json |
| errors.TimeoutErrorT       | 504                          | application/json |
| errors.NotFoundError       | 501, 505                     | application/json |
| errors.InternalServerError | 500, 502, 503, 506, 507, 508 | application/json |
| errors.BadRequestError     | 510                          | application/json |
| errors.UnauthorizedError   | 511                          | application/json |
| errors.APIError            | 4XX, 5XX                     | \*/\*            |

### Example

```python
from openapi import SDK, errors


with SDK() as sdk:
    res = None
    try:

        res = sdk.health.check()

        # Handle response
        print(res)

    except errors.NotFoundError as e:
        # handle e.data: errors.NotFoundErrorData
        raise(e)
    except errors.UnauthorizedError as e:
        # handle e.data: errors.UnauthorizedErrorData
        raise(e)
    except errors.TimeoutErrorT as e:
        # handle e.data: errors.TimeoutErrorTData
        raise(e)
    except errors.RateLimitedError as e:
        # handle e.data: errors.RateLimitedErrorData
        raise(e)
    except errors.BadRequestError as e:
        # handle e.data: errors.BadRequestErrorData
        raise(e)
    except errors.TimeoutErrorT as e:
        # handle e.data: errors.TimeoutErrorTData
        raise(e)
    except errors.NotFoundError as e:
        # handle e.data: errors.NotFoundErrorData
        raise(e)
    except errors.InternalServerError as e:
        # handle e.data: errors.InternalServerErrorData
        raise(e)
    except errors.BadRequestError as e:
        # handle e.data: errors.BadRequestErrorData
        raise(e)
    except errors.UnauthorizedError as e:
        # handle e.data: errors.UnauthorizedErrorData
        raise(e)
    except errors.APIError as e:
        # handle exception
        raise(e)
```
<!-- End Error Handling [errors] -->

<!-- Start Server Selection [server] -->
## Server Selection

### Override Server URL Per-Client

The default server can be overridden globally by passing a URL to the `server_url: str` optional parameter when initializing the SDK client instance. For example:
```python
from openapi import SDK


with SDK(
    server_url="http://localhost:3333/api/v1",
) as sdk:

    res = sdk.health.check()

    # Handle response
    print(res)

```
<!-- End Server Selection [server] -->

<!-- Start Custom HTTP Client [http-client] -->
## Custom HTTP Client

The Python SDK makes API calls using the [httpx](https://www.python-httpx.org/) HTTP library.  In order to provide a convenient way to configure timeouts, cookies, proxies, custom headers, and other low-level configuration, you can initialize the SDK client with your own HTTP client instance.
Depending on whether you are using the sync or async version of the SDK, you can pass an instance of `HttpClient` or `AsyncHttpClient` respectively, which are Protocol's ensuring that the client has the necessary methods to make API calls.
This allows you to wrap the client with your own custom logic, such as adding custom headers, logging, or error handling, or you can just pass an instance of `httpx.Client` or `httpx.AsyncClient` directly.

For example, you could specify a header for every request that this sdk makes as follows:
```python
from openapi import SDK
import httpx

http_client = httpx.Client(headers={"x-custom-header": "someValue"})
s = SDK(client=http_client)
```

or you could wrap the client with your own custom logic:
```python
from openapi import SDK
from openapi.httpclient import AsyncHttpClient
import httpx

class CustomClient(AsyncHttpClient):
    client: AsyncHttpClient

    def __init__(self, client: AsyncHttpClient):
        self.client = client

    async def send(
        self,
        request: httpx.Request,
        *,
        stream: bool = False,
        auth: Union[
            httpx._types.AuthTypes, httpx._client.UseClientDefault, None
        ] = httpx.USE_CLIENT_DEFAULT,
        follow_redirects: Union[
            bool, httpx._client.UseClientDefault
        ] = httpx.USE_CLIENT_DEFAULT,
    ) -> httpx.Response:
        request.headers["Client-Level-Header"] = "added by client"

        return await self.client.send(
            request, stream=stream, auth=auth, follow_redirects=follow_redirects
        )

    def build_request(
        self,
        method: str,
        url: httpx._types.URLTypes,
        *,
        content: Optional[httpx._types.RequestContent] = None,
        data: Optional[httpx._types.RequestData] = None,
        files: Optional[httpx._types.RequestFiles] = None,
        json: Optional[Any] = None,
        params: Optional[httpx._types.QueryParamTypes] = None,
        headers: Optional[httpx._types.HeaderTypes] = None,
        cookies: Optional[httpx._types.CookieTypes] = None,
        timeout: Union[
            httpx._types.TimeoutTypes, httpx._client.UseClientDefault
        ] = httpx.USE_CLIENT_DEFAULT,
        extensions: Optional[httpx._types.RequestExtensions] = None,
    ) -> httpx.Request:
        return self.client.build_request(
            method,
            url,
            content=content,
            data=data,
            files=files,
            json=json,
            params=params,
            headers=headers,
            cookies=cookies,
            timeout=timeout,
            extensions=extensions,
        )

s = SDK(async_client=CustomClient(httpx.AsyncClient()))
```
<!-- End Custom HTTP Client [http-client] -->

<!-- Start Resource Management [resource-management] -->
## Resource Management

The `SDK` class implements the context manager protocol and registers a finalizer function to close the underlying sync and async HTTPX clients it uses under the hood. This will close HTTP connections, release memory and free up other resources held by the SDK. In short-lived Python programs and notebooks that make a few SDK method calls, resource management may not be a concern. However, in longer-lived programs, it is beneficial to create a single SDK instance via a [context manager][context-manager] and reuse it across the application.

[context-manager]: https://docs.python.org/3/reference/datamodel.html#context-managers

```python
from openapi import SDK
def main():

    with SDK() as sdk:
        # Rest of application here...


# Or when using async:
async def amain():

    async with SDK() as sdk:
        # Rest of application here...
```
<!-- End Resource Management [resource-management] -->

<!-- Start Debugging [debug] -->
## Debugging

You can setup your SDK to emit debug logs for SDK requests and responses.

You can pass your own logger class directly into your SDK.
```python
from openapi import SDK
import logging

logging.basicConfig(level=logging.DEBUG)
s = SDK(debug_logger=logging.getLogger("openapi"))
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

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=openapi&utm_campaign=python)
