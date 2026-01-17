# Tenants

## Overview

The API segments resources per `tenant`. A tenant represents a user/team/organization in your product. The provided value determines the tenant's ID, which can be any string representation.

If your system is not multi-tenant, create a single tenant with a hard-code tenant ID upon initialization. If your system has a single tenant but multiple environments, create a tenant per environment, like `live` and `test`.


### Available Operations

* [list_tenants](#list_tenants) - List Tenants
* [upsert](#upsert) - Create or Update Tenant
* [get](#get) - Get Tenant
* [delete](#delete) - Delete Tenant
* [get_portal_url](#get_portal_url) - Get Portal Redirect URL
* [get_token](#get_token) - Get Tenant JWT Token

## list_tenants

List all tenants with cursor-based pagination.

**Requirements:** This endpoint requires Redis with RediSearch module (e.g., `redis/redis-stack-server`).
If RediSearch is not available, this endpoint returns `501 Not Implemented`.

The response includes lightweight tenant objects without computed fields like `destinations_count` and `topics`.
Use `GET /tenants/{tenant_id}` to retrieve full tenant details including these fields.


### Example Usage

<!-- UsageSnippet language="python" operationID="listTenants" method="get" path="/tenants" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.tenants.list_tenants(limit=20, order=models.Order.DESC)

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `limit`                                                                  | *Optional[int]*                                                          | :heavy_minus_sign:                                                       | Number of tenants to return per page (1-100, default 20).                |
| `order`                                                                  | [Optional[models.Order]](../../models/order.md)                          | :heavy_minus_sign:                                                       | Sort order by `created_at` timestamp.                                    |
| `next_cursor`                                                            | *Optional[str]*                                                          | :heavy_minus_sign:                                                       | Cursor for the next page of results. Mutually exclusive with `prev`.     |
| `prev_cursor`                                                            | *Optional[str]*                                                          | :heavy_minus_sign:                                                       | Cursor for the previous page of results. Mutually exclusive with `next`. |
| `retries`                                                                | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)         | :heavy_minus_sign:                                                       | Configuration to override the default retry behavior of the client.      |

### Response

**[models.TenantListResponse](../../models/tenantlistresponse.md)**

### Errors

| Error Type                        | Status Code                       | Content Type                      |
| --------------------------------- | --------------------------------- | --------------------------------- |
| errors.ListTenantsBadRequestError | 400                               | application/json                  |
| errors.NotImplementedErrorT       | 501                               | application/json                  |
| errors.APIError                   | 4XX, 5XX                          | \*/\*                             |

## upsert

Idempotently creates or updates a tenant. Required before associating destinations.

### Example Usage

<!-- UsageSnippet language="python" operationID="upsertTenant" method="put" path="/tenants/{tenant_id}" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.tenants.upsert()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `params`                                                              | [Optional[models.TenantUpsert]](../../models/tenantupsert.md)         | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Tenant](../../models/tenant.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## get

Retrieves details for a specific tenant.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenant" method="get" path="/tenants/{tenant_id}" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.tenants.get()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.Tenant](../../models/tenant.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## delete

Deletes the tenant and all associated destinations.

### Example Usage

<!-- UsageSnippet language="python" operationID="deleteTenant" method="delete" path="/tenants/{tenant_id}" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.tenants.delete()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.SuccessResponse](../../models/successresponse.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## get_portal_url

Returns a redirect URL containing a JWT to authenticate the user with the portal.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantPortalUrl" method="get" path="/tenants/{tenant_id}/portal" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.tenants.get_portal_url()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `theme`                                                               | [Optional[models.Theme]](../../models/theme.md)                       | :heavy_minus_sign:                                                    | Optional theme preference for the portal.                             |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.PortalRedirect](../../models/portalredirect.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |

## get_token

Returns a JWT token scoped to the tenant for safe browser API calls.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantToken" method="get" path="/tenants/{tenant_id}/token" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.tenants.get_token()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[models.TenantToken](../../models/tenanttoken.md)**

### Errors

| Error Type      | Status Code     | Content Type    |
| --------------- | --------------- | --------------- |
| errors.APIError | 4XX, 5XX        | \*/\*           |