# Configuration

## Overview

Outpost instance-level configuration management for the managed deployment.
This is not available in self-hosted deployments.


### Available Operations

* [GetManagedConfig](#getmanagedconfig) - Get Managed Configuration
* [UpdateManagedConfig](#updatemanagedconfig) - Update Managed Configuration

## GetManagedConfig

Returns managed Outpost configuration values.

This endpoint is only available for the managed version.
In self-hosted deployments, configuration is controlled through environment variables instead.


### Example Usage

<!-- UsageSnippet language="go" operationID="getManagedConfig" method="get" path="/config" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Configuration.GetManagedConfig(ctx)
    if err != nil {
        log.Fatal(err)
    }
    if res.ManagedConfig != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `ctx`                                                    | [context.Context](https://pkg.go.dev/context#Context)    | :heavy_check_mark:                                       | The context to use for the request.                      |
| `opts`                                                   | [][operations.Option](../../models/operations/option.md) | :heavy_minus_sign:                                       | The options for this request.                            |

### Response

**[*operations.GetManagedConfigResponse](../../models/operations/getmanagedconfigresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |

## UpdateManagedConfig

Updates one or more managed Outpost configuration values. Null values clear the configuration and reverts to Outpost default behavior.

This endpoint is only available for the managed version.
In self-hosted deployments, configuration is controlled through environment variables instead.

Only the supported configuration keys are accepted.


### Example Usage

<!-- UsageSnippet language="go" operationID="updateManagedConfig" method="patch" path="/config" -->
```go
package main

import(
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
    ctx := context.Background()

    s := outpostgo.New(
        outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Configuration.UpdateManagedConfig(ctx, components.ManagedConfig{
        DestinationsWebhookMode: outpostgo.Pointer("default"),
        Topics: outpostgo.Pointer("user.created,user.updated"),
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.ManagedConfig != nil {
        // handle response
    }
}
```

### Parameters

| Parameter                                                            | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `ctx`                                                                | [context.Context](https://pkg.go.dev/context#Context)                | :heavy_check_mark:                                                   | The context to use for the request.                                  |
| `request`                                                            | [components.ManagedConfig](../../models/components/managedconfig.md) | :heavy_check_mark:                                                   | The request object to use for the request.                           |
| `opts`                                                               | [][operations.Option](../../models/operations/option.md)             | :heavy_minus_sign:                                                   | The options for this request.                                        |

### Response

**[*operations.UpdateManagedConfigResponse](../../models/operations/updatemanagedconfigresponse.md), error**

### Errors

| Error Type                    | Status Code                   | Content Type                  |
| ----------------------------- | ----------------------------- | ----------------------------- |
| apierrors.BadRequestError     | 400                           | application/json              |
| apierrors.UnauthorizedError   | 401                           | application/json              |
| apierrors.APIErrorResponse    | 422                           | application/json              |
| apierrors.InternalServerError | 500                           | application/json              |
| apierrors.APIError            | 4XX, 5XX                      | \*/\*                         |