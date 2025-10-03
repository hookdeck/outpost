# Outpost Go Client

Developer-friendly & type-safe Go client specifically catered to leverage the Outpost API.

<div align="left">
    <a href="https://www.speakeasy.com/?utm_source=github-com/hookdeck/outpost/sdks/outpost-go&utm_campaign=go"><img src="https://custom-icon-badges.demolab.com/badge/-Built%20By%20Speakeasy-212015?style=for-the-badge&logoColor=FBE331&logo=speakeasy&labelColor=545454" /></a>
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
* [Outpost Go Client](#outpost-go-client)
  * [SDK Installation](#sdk-installation)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
  * [Global Parameters](#global-parameters)
  * [Retries](#retries)
  * [Error Handling](#error-handling)
  * [Server Selection](#server-selection)
  * [Custom HTTP Client](#custom-http-client)
* [Development](#development)
  * [Maturity](#maturity)
  * [Contributions](#contributions)

<!-- End Table of Contents [toc] -->

<!-- Start SDK Installation [installation] -->
## SDK Installation

To add the SDK as a dependency to your project:
```bash
go get github.com/hookdeck/outpost/sdks/outpost-go
```
<!-- End SDK Installation [installation] -->

<!-- Start SDK Example Usage [usage] -->
## SDK Example Usage

### Example

```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New()

	res, err := s.Health.Check(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.Res != nil {
		// handle response
	}
}

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security schemes globally:

| Name          | Type | Scheme      |
| ------------- | ---- | ----------- |
| `AdminAPIKey` | http | HTTP Bearer |
| `TenantJwt`   | http | HTTP Bearer |

You can set the security parameters through the `WithSecurity` option when initializing the SDK client instance. The selected scheme will be used by default to authenticate with the API for all operations that support it. For example:
```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithSecurity(components.Security{
			AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
		}),
	)

	res, err := s.Health.Check(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.Res != nil {
		// handle response
	}
}

```
<!-- End Authentication [security] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [Destinations](docs/sdks/destinations/README.md)

* [List](docs/sdks/destinations/README.md#list) - List Destinations
* [Create](docs/sdks/destinations/README.md#create) - Create Destination
* [Get](docs/sdks/destinations/README.md#get) - Get Destination
* [Update](docs/sdks/destinations/README.md#update) - Update Destination
* [Delete](docs/sdks/destinations/README.md#delete) - Delete Destination
* [Enable](docs/sdks/destinations/README.md#enable) - Enable Destination
* [Disable](docs/sdks/destinations/README.md#disable) - Disable Destination

### [Events](docs/sdks/events/README.md)

* [List](docs/sdks/events/README.md#list) - List Events
* [Get](docs/sdks/events/README.md#get) - Get Event
* [ListDeliveries](docs/sdks/events/README.md#listdeliveries) - List Event Delivery Attempts
* [ListByDestination](docs/sdks/events/README.md#listbydestination) - List Events by Destination
* [GetByDestination](docs/sdks/events/README.md#getbydestination) - Get Event by Destination
* [Retry](docs/sdks/events/README.md#retry) - Retry Event Delivery

### [Health](docs/sdks/health/README.md)

* [Check](docs/sdks/health/README.md#check) - Health Check


### [Publish](docs/sdks/publish/README.md)

* [Event](docs/sdks/publish/README.md#event) - Publish Event

### [Schemas](docs/sdks/schemas/README.md)

* [ListTenantDestinationTypes](docs/sdks/schemas/README.md#listtenantdestinationtypes) - List Destination Type Schemas (for Tenant)
* [Get](docs/sdks/schemas/README.md#get) - Get Destination Type Schema (for Tenant)
* [ListDestinationTypesJwt](docs/sdks/schemas/README.md#listdestinationtypesjwt) - List Destination Type Schemas (JWT Auth)
* [GetDestinationTypeJwt](docs/sdks/schemas/README.md#getdestinationtypejwt) - Get Destination Type Schema

### [Tenants](docs/sdks/tenants/README.md)

* [Upsert](docs/sdks/tenants/README.md#upsert) - Create or Update Tenant
* [Get](docs/sdks/tenants/README.md#get) - Get Tenant
* [Delete](docs/sdks/tenants/README.md#delete) - Delete Tenant
* [GetPortalURL](docs/sdks/tenants/README.md#getportalurl) - Get Portal Redirect URL
* [GetToken](docs/sdks/tenants/README.md#gettoken) - Get Tenant JWT Token

### [Topics](docs/sdks/topics/README.md)

* [List](docs/sdks/topics/README.md#list) - List Available Topics (for Tenant)
* [ListJwt](docs/sdks/topics/README.md#listjwt) - List Available Topics)

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start Global Parameters [global-parameters] -->
## Global Parameters

A parameter is configured globally. This parameter may be set on the SDK client instance itself during initialization. When configured as an option during SDK initialization, This global value will be used as the default on the operations that use it. When such operations are called, there is a place in each to override the global value, if needed.

For example, you can set `tenant_id` to `"<id>"` at SDK initialization and then you do not have to pass the same value on calls to operations like `Upsert`. But if you want to do so you may, which will locally override the global setting. See the example code below for a demonstration.


### Available Globals

The following global parameter is available.

| Name     | Type   | Description             |
| -------- | ------ | ----------------------- |
| TenantID | string | The TenantID parameter. |

### Example

```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithTenantID("<id>"),
		outpostgo.WithSecurity(components.Security{
			AdminAPIKey: outpostgo.String("<YOUR_BEARER_TOKEN_HERE>"),
		}),
	)

	res, err := s.Tenants.Upsert(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.Tenant != nil {
		// handle response
	}
}

```
<!-- End Global Parameters [global-parameters] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries. If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API. However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a `retry.Config` object to the call by using the `WithRetries` option:
```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/retry"
	"log"
	"models/operations"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New()

	res, err := s.Health.Check(ctx, operations.WithRetries(
		retry.Config{
			Strategy: "backoff",
			Backoff: &retry.BackoffStrategy{
				InitialInterval: 1,
				MaxInterval:     50,
				Exponent:        1.1,
				MaxElapsedTime:  100,
			},
			RetryConnectionErrors: false,
		}))
	if err != nil {
		log.Fatal(err)
	}
	if res.Res != nil {
		// handle response
	}
}

```

If you'd like to override the default retry strategy for all operations that support retries, you can use the `WithRetryConfig` option at SDK initialization:
```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/retry"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithRetryConfig(
			retry.Config{
				Strategy: "backoff",
				Backoff: &retry.BackoffStrategy{
					InitialInterval: 1,
					MaxInterval:     50,
					Exponent:        1.1,
					MaxElapsedTime:  100,
				},
				RetryConnectionErrors: false,
			}),
	)

	res, err := s.Health.Check(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.Res != nil {
		// handle response
	}
}

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

Handling errors in this SDK should largely match your expectations. All operations return a response object or an error, they will never return both.

By Default, an API error will return `apierrors.APIError`. When custom error responses are specified for an operation, the SDK may also return their associated error. You can refer to respective *Errors* tables in SDK docs for more details on possible error types for each operation.

For example, the `Check` function may return the following errors:

| Error Type                    | Status Code                  | Content Type     |
| ----------------------------- | ---------------------------- | ---------------- |
| apierrors.NotFoundError       | 404                          | application/json |
| apierrors.UnauthorizedError   | 401, 403, 407                | application/json |
| apierrors.TimeoutError        | 408                          | application/json |
| apierrors.RateLimitedError    | 429                          | application/json |
| apierrors.BadRequestError     | 400, 413, 414, 415, 422, 431 | application/json |
| apierrors.TimeoutError        | 504                          | application/json |
| apierrors.NotFoundError       | 501, 505                     | application/json |
| apierrors.InternalServerError | 500, 502, 503, 506, 507, 508 | application/json |
| apierrors.BadRequestError     | 510                          | application/json |
| apierrors.UnauthorizedError   | 511                          | application/json |
| apierrors.APIError            | 4XX, 5XX                     | \*/\*            |

### Example

```go
package main

import (
	"context"
	"errors"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/apierrors"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New()

	res, err := s.Health.Check(ctx)
	if err != nil {

		var e *apierrors.NotFoundError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.UnauthorizedError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.TimeoutError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.RateLimitedError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.BadRequestError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.TimeoutError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.NotFoundError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.InternalServerError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.BadRequestError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.UnauthorizedError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}

		var e *apierrors.APIError
		if errors.As(err, &e) {
			// handle error
			log.Fatal(e.Error())
		}
	}
}

```
<!-- End Error Handling [errors] -->

<!-- Start Server Selection [server] -->
## Server Selection

### Override Server URL Per-Client

The default server can be overridden globally using the `WithServerURL(serverURL string)` option when initializing the SDK client instance. For example:
```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithServerURL("http://localhost:3333/api/v1"),
	)

	res, err := s.Health.Check(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if res.Res != nil {
		// handle response
	}
}

```
<!-- End Server Selection [server] -->

<!-- Start Custom HTTP Client [http-client] -->
## Custom HTTP Client

The Go SDK makes API calls that wrap an internal HTTP client. The requirements for the HTTP client are very simple. It must match this interface:

```go
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}
```

The built-in `net/http` client satisfies this interface and a default client based on the built-in is provided by default. To replace this default with a client of your own, you can implement this interface yourself or provide your own client configured as desired. Here's a simple example, which adds a client with a 30 second timeout.

```go
import (
	"net/http"
	"time"

	"github.com/hookdeck/outpost/sdks/outpost-go"
)

var (
	httpClient = &http.Client{Timeout: 30 * time.Second}
	sdkClient  = outpostgo.New(outpostgo.WithClient(httpClient))
)
```

This can be a convenient way to configure timeouts, cookies, proxies, custom headers, and other low-level configuration.
<!-- End Custom HTTP Client [http-client] -->

<!-- Placeholder for Future Speakeasy SDK Sections -->

# Development

## Maturity

This SDK is in beta, and there may be breaking changes between versions without a major version update. Therefore, we recommend pinning usage
to a specific package version. This way, you can install the same version each time without breaking changes unless you are intentionally
looking for the latest version.

## Contributions

While we value open-source contributions to this SDK, this library is generated programmatically. Any manual changes added to internal files will be overwritten on the next generation. 
We look forward to hearing your feedback. Feel free to open a PR or an issue with a proof of concept and we'll do our best to include it in a future release. 

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=github-com/hookdeck/outpost/sdks/outpost-go&utm_campaign=go)
