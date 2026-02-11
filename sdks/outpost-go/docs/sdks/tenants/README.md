# Tenants

## Overview

The API segments resources per `tenant`. A tenant represents a user/team/organization in your product. The provided value determines the tenant's ID, which can be any string representation.

If your system is not multi-tenant, create a single tenant with a hard-code tenant ID upon initialization. If your system has a single tenant but multiple environments, create a tenant per environment, like `live` and `test`.


### Available Operations

* [ListTenants](#listtenants) - List Tenants
* [Upsert](#upsert) - Create or Update Tenant
* [Get](#get) - Get Tenant
* [Delete](#delete) - Delete Tenant
* [GetPortalURL](#getportalurl) - Get Portal Redirect URL
* [GetToken](#gettoken) - Get Tenant JWT Token

## ListTenants

List all tenants with cursor-based pagination.

**Requirements:** This endpoint requires Redis with RediSearch module (e.g., `redis/redis-stack-server`).
If RediSearch is not available, this endpoint returns `501 Not Implemented`.

The response includes lightweight tenant objects without computed fields like `destinations_count` and `topics`.
Use `GET /tenants/{tenant_id}` to retrieve full tenant details including these fields.


### Example Usage

<!-- UsageSnippet language="go" operationID="listTenants" method="get" path="/tenants" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/operations"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Tenants.ListTenants(ctx, operations.ListTenantsRequest{})
    if err != nil {
        log.Fatal(err)
    }
    if res.TenantPaginatedResult != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                                      | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `ctx`                                                                          | [context.Context](https://pkg.go.dev/context#Context)                          | :heavy_check_mark:                                                             | The context to use for the request.                                            |
| `request`                                                                      | [operations.ListTenantsRequest](../../models/operations/listtenantsrequest.md) | :heavy_check_mark:                                                             | The request object to use for the request.                                     |
| `opts`                                                                         | [][operations.Option](../../models/operations/option.md)                       | :heavy_minus_sign:                                                             | The options for this request.                                                  |

### Response

**[*operations.ListTenantsResponse](../../models/operations/listtenantsresponse.md), error**

### Errors

| Error Type                           | Status Code                          | Content Type                         |
| ------------------------------------ | ------------------------------------ | ------------------------------------ |
| apierrors.ListTenantsBadRequestError | 400                                  | application/json                     |
| apierrors.NotImplementedError        | 501                                  | application/json                     |
| apierrors.APIError                   | 4XX, 5XX                             | \*/\*                                |

## Upsert

Idempotently creates or updates a tenant. Required before associating destinations.

### Example Usage

<!-- UsageSnippet language="go" operationID="upsertTenant" method="put" path="/tenants/{tenant_id}" example="TenantExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Tenants.Upsert(ctx, nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.Tenant != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `params`                                                              | [*components.TenantUpsert](../../models/components/tenantupsert.md)   | :heavy_minus_sign:                                                    | Optional tenant metadata                                              |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.UpsertTenantResponse](../../models/operations/upserttenantresponse.md), error**

### Errors

| Error Type         | Status Code        | Content Type       |
| ------------------ | ------------------ | ------------------ |
| apierrors.APIError | 4XX, 5XX           | \*/\*              |

## Get

Retrieves details for a specific tenant.

### Example Usage

<!-- UsageSnippet language="go" operationID="getTenant" method="get" path="/tenants/{tenant_id}" example="TenantExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Tenants.Get(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.Tenant != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.GetTenantResponse](../../models/operations/gettenantresponse.md), error**

### Errors

| Error Type         | Status Code        | Content Type       |
| ------------------ | ------------------ | ------------------ |
| apierrors.APIError | 4XX, 5XX           | \*/\*              |

## Delete

Deletes the tenant and all associated destinations.

### Example Usage

<!-- UsageSnippet language="go" operationID="deleteTenant" method="delete" path="/tenants/{tenant_id}" example="SuccessExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Tenants.Delete(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.SuccessResponse != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.DeleteTenantResponse](../../models/operations/deletetenantresponse.md), error**

### Errors

| Error Type         | Status Code        | Content Type       |
| ------------------ | ------------------ | ------------------ |
| apierrors.APIError | 4XX, 5XX           | \*/\*              |

## GetPortalURL

Returns a redirect URL containing a JWT to authenticate the user with the portal.

### Example Usage

<!-- UsageSnippet language="go" operationID="getTenantPortalUrl" method="get" path="/tenants/{tenant_id}/portal" example="PortalRedirectExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Tenants.GetPortalURL(ctx, nil)
    if err != nil {
        log.Fatal(err)
    }
    if res.PortalRedirect != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `theme`                                                               | [*operations.Theme](../../models/operations/theme.md)                 | :heavy_minus_sign:                                                    | Optional theme preference for the portal.                             |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.GetTenantPortalURLResponse](../../models/operations/gettenantportalurlresponse.md), error**

### Errors

| Error Type         | Status Code        | Content Type       |
| ------------------ | ------------------ | ------------------ |
| apierrors.APIError | 4XX, 5XX           | \*/\*              |

## GetToken

Returns a JWT token scoped to the tenant for safe browser API calls.

### Example Usage

<!-- UsageSnippet language="go" operationID="getTenantToken" method="get" path="/tenants/{tenant_id}/token" example="TenantTokenExample" -->
```go
package main

import(
	"context"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithTenantID("<id>"),
        outpostgo.WithSecurity(components.Security{
            AdminAPIKey: outpostgo.Pointer("<YOUR_BEARER_TOKEN_HERE>"),
        }),
    )

    res, err := s.Tenants.GetToken(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.TenantToken != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `ctx`                                                                 | [context.Context](https://pkg.go.dev/context#Context)                 | :heavy_check_mark:                                                    | The context to use for the request.                                   |
| `tenantID`                                                            | **string*                                                             | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `opts`                                                                | [][operations.Option](../../models/operations/option.md)              | :heavy_minus_sign:                                                    | The options for this request.                                         |

### Response

**[*operations.GetTenantTokenResponse](../../models/operations/gettenanttokenresponse.md), error**

### Errors

| Error Type         | Status Code        | Content Type       |
| ------------------ | ------------------ | ------------------ |
| apierrors.APIError | 4XX, 5XX           | \*/\*              |