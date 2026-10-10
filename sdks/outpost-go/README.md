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

Outpost runs in two deployment models: **managed** (hosted by Hookdeck) and **self-hosted**. They differ in where the API is served and in which API key authenticates server-side calls. On managed Outpost, use a Hookdeck project API key from your Outpost project. On self-hosted Outpost, use the key set in the `API_KEY` environment variable. A few endpoints exist in one model only and say so in their description.
<!-- End Summary [summary] -->

<!-- Start Table of Contents [toc] -->
## Table of Contents
<!-- $toc-max-depth=2 -->
* [Outpost Go Client](#outpost-go-client)
  * [SDK Installation](#sdk-installation)
  * [SDK Example Usage](#sdk-example-usage)
  * [Authentication](#authentication)
  * [Available Resources and Operations](#available-resources-and-operations)
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
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.PublishResponse != nil {
		// handle response
	}
}

```
<!-- End SDK Example Usage [usage] -->

<!-- Start Authentication [security] -->
## Authentication

### Per-Client Security Schemes

This SDK supports the following security scheme globally:

| Name     | Type | Scheme      |
| -------- | ---- | ----------- |
| `APIKey` | http | HTTP Bearer |

You can configure it using the `WithSecurity` option when initializing the SDK client instance. For example:
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
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.PublishResponse != nil {
		// handle response
	}
}

```
<!-- End Authentication [security] -->

<!-- Start Available Resources and Operations [operations] -->
## Available Resources and Operations

<details open>
<summary>Available methods</summary>

### [Outpost SDK](docs/sdks/outpost/README.md)

* [Publish](docs/sdks/outpost/README.md#publish) - Publish Event
* [Retry](docs/sdks/outpost/README.md#retry) - Retry Event Delivery

### [Attempts](docs/sdks/attempts/README.md)

* [List](docs/sdks/attempts/README.md#list) - List Attempts
* [Get](docs/sdks/attempts/README.md#get) - Get Attempt

### [Configuration](docs/sdks/configuration/README.md)

* [GetManagedConfig](docs/sdks/configuration/README.md#getmanagedconfig) - Get Managed Configuration
* [UpdateManagedConfig](docs/sdks/configuration/README.md#updatemanagedconfig) - Update Managed Configuration

### [Destinations](docs/sdks/destinations/README.md)

* [List](docs/sdks/destinations/README.md#list) - List Destinations
* [Create](docs/sdks/destinations/README.md#create) - Create Destination
* [Get](docs/sdks/destinations/README.md#get) - Get Destination
* [Update](docs/sdks/destinations/README.md#update) - Update Destination
* [Delete](docs/sdks/destinations/README.md#delete) - Delete Destination
* [Enable](docs/sdks/destinations/README.md#enable) - Enable Destination
* [Disable](docs/sdks/destinations/README.md#disable) - Disable Destination
* [ListAttempts](docs/sdks/destinations/README.md#listattempts) - List Destination Attempts
* [GetAttempt](docs/sdks/destinations/README.md#getattempt) - Get Destination Attempt

### [Events](docs/sdks/events/README.md)

* [List](docs/sdks/events/README.md#list) - List Events
* [Get](docs/sdks/events/README.md#get) - Get Event

### [Health](docs/sdks/health/README.md)

* [Check](docs/sdks/health/README.md#check) - Health Check

### [Metrics](docs/sdks/metrics/README.md)

* [GetEventMetrics](docs/sdks/metrics/README.md#geteventmetrics) - Get Event Metrics
* [GetAttemptMetrics](docs/sdks/metrics/README.md#getattemptmetrics) - Get Attempt Metrics

### [OperatorEvents](docs/sdks/operatorevents/README.md)

* [ListDestinationTypes](docs/sdks/operatorevents/README.md#listdestinationtypes) - List Operator Event Destination Type Schemas
* [ListDestinations](docs/sdks/operatorevents/README.md#listdestinations) - List Operator Event Destinations
* [CreateDestination](docs/sdks/operatorevents/README.md#createdestination) - Create Operator Event Destination
* [GetDestination](docs/sdks/operatorevents/README.md#getdestination) - Get Operator Event Destination
* [UpdateDestination](docs/sdks/operatorevents/README.md#updatedestination) - Update Operator Event Destination
* [DeleteDestination](docs/sdks/operatorevents/README.md#deletedestination) - Delete Operator Event Destination
* [EnableDestination](docs/sdks/operatorevents/README.md#enabledestination) - Enable Operator Event Destination
* [DisableDestination](docs/sdks/operatorevents/README.md#disabledestination) - Disable Operator Event Destination
* [ListEvents](docs/sdks/operatorevents/README.md#listevents) - List Operator Events
* [GetEvent](docs/sdks/operatorevents/README.md#getevent) - Get Operator Event
* [ListEventAttempts](docs/sdks/operatorevents/README.md#listeventattempts) - List Attempts for an Operator Event
* [ListAttempts](docs/sdks/operatorevents/README.md#listattempts) - List Operator Event Attempts
* [GetAttempt](docs/sdks/operatorevents/README.md#getattempt) - Get Operator Event Attempt
* [Retry](docs/sdks/operatorevents/README.md#retry) - Retry Operator Event Delivery

### [Schemas](docs/sdks/schemas/README.md)

* [ListDestinationTypes](docs/sdks/schemas/README.md#listdestinationtypes) - List Destination Type Schemas
* [GetDestinationType](docs/sdks/schemas/README.md#getdestinationtype) - Get Destination Type Schema

### [Tenants](docs/sdks/tenants/README.md)

* [List](docs/sdks/tenants/README.md#list) - List Tenants
* [Upsert](docs/sdks/tenants/README.md#upsert) - Create or Update Tenant
* [Get](docs/sdks/tenants/README.md#get) - Get Tenant
* [Delete](docs/sdks/tenants/README.md#delete) - Delete Tenant
* [GetPortalURL](docs/sdks/tenants/README.md#getportalurl) - Get Portal Redirect URL
* [GetToken](docs/sdks/tenants/README.md#gettoken) - Get Tenant JWT Token

### [Topics](docs/sdks/topics/README.md)

* [List](docs/sdks/topics/README.md#list) - List Available Topics

</details>
<!-- End Available Resources and Operations [operations] -->

<!-- Start Retries [retries] -->
## Retries

Some of the endpoints in this SDK support retries. If you use the SDK without any configuration, it will fall back to the default retry strategy provided by the API. However, the default retry strategy can be overridden on a per-operation basis, or across the entire SDK.

To change the default retry strategy for a single API call, simply provide a `retry.Config` object to the call by using the `WithRetries` option:
```go
package main

import (
	"context"
	outpostgo "github.com/hookdeck/outpost/sdks/outpost-go"
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"github.com/hookdeck/outpost/sdks/outpost-go/retry"
	"log"
	"models/operations"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	}, operations.WithRetries(
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
	if res.PublishResponse != nil {
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
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
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
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.PublishResponse != nil {
		// handle response
	}
}

```
<!-- End Retries [retries] -->

<!-- Start Error Handling [errors] -->
## Error Handling

Handling errors in this SDK should largely match your expectations. All operations return a response object or an error, they will never return both.

By Default, an API error will return `apierrors.APIError`. When custom error responses are specified for an operation, the SDK may also return their associated error. You can refer to respective *Errors* tables in SDK docs for more details on possible error types for each operation.

For example, the `Publish` function may return the following errors:

| Error Type                    | Status Code                  | Content Type     |
| ----------------------------- | ---------------------------- | ---------------- |
| apierrors.NotFoundError       | 404                          | application/json |
| apierrors.UnauthorizedError   | 401, 403, 407                | application/json |
| apierrors.TimeoutError        | 408                          | application/json |
| apierrors.APIErrorResponse    | 409                          | application/json |
| apierrors.RateLimitedError    | 429                          | application/json |
| apierrors.BadRequestError     | 413, 414, 415, 422, 431      | application/json |
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
	"github.com/hookdeck/outpost/sdks/outpost-go/models/components"
	"log"
)

func main() {
	ctx := context.Background()

	s := outpostgo.New(
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	})
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

		var e *apierrors.APIErrorResponse
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

### Select Server by Index

You can override the default server globally using the `WithServerIndex(serverIndex int)` option when initializing the SDK client instance. The selected server will then be used as the default on the operations that use it. This table lists the indexes associated with the available servers:

| #   | Server                                        | Description                                                                                                                                |
| --- | --------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------ |
| 0   | `https://api.outpost.hookdeck.com/2025-07-01` | Managed Outpost, hosted by Hookdeck at `api.outpost.hookdeck.com`. The Hookdeck Event Gateway API at `api.hookdeck.com` is a separate API. |
| 1   | `http://localhost:3333/api/v1`                | Self-hosted Outpost, at its default local address. A deployed instance serves the same paths under its own host.                           |

#### Example

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
		outpostgo.WithServerIndex(0),
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.PublishResponse != nil {
		// handle response
	}
}

```

### Override Server URL Per-Client

The default server can also be overridden globally using the `WithServerURL(serverURL string)` option when initializing the SDK client instance. For example:
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
		outpostgo.WithServerURL("http://localhost:3333/api/v1"),
		outpostgo.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
	)

	res, err := s.Publish(ctx, components.PublishRequest{
		ID:               outpostgo.Pointer("evt_abc123xyz789"),
		TenantID:         outpostgo.Pointer("tenant_123"),
		Topic:            outpostgo.Pointer("user.created"),
		EligibleForRetry: outpostgo.Pointer(true),
		Metadata: map[string]string{
			"source": "crm",
		},
		Data: map[string]any{
			"user_id": "userid",
			"status":  "active",
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	if res.PublishResponse != nil {
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

# MCP Events

Outpost runs [MCP Events](https://hookdeck.com/docs/outpost/guides/mcp-events) for your MCP server: the server answers `events/list`, `events/subscribe` and `events/unsubscribe` by forwarding them to three Outpost endpoints, and Outpost verifies callbacks, stores subscriptions and delivers events. Package `mcpevents` ships that glue, using only the standard library; package `mcpevents/mcpsdk` plugs it into the [official Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk). The MCP endpoints exist in Outpost API v2 only: use a v2 base URL, such as `http://localhost:3333/api/v2` (a base URL ending in `/api/v1` is rewritten to `/api/v2`).

## Drop-in registration

`mcpsdk.Register` adds the three methods to an `*mcp.Server` and advertises the `events` capability (the design-sketch `events: {}` that ChatGPT reads and the SEP-3415 `extensions` entry). Call it before the server serves any request. The SDK serves the `2026-07-28` revision ChatGPT requires through a stateless `StreamableHTTPHandler`.

`mcpsdk` is a module of its own, so only the applications that use it depend on the Go MCP SDK. Add it next to this SDK:

```bash
go get github.com/hookdeck/outpost/sdks/outpost-go/mcpevents/mcpsdk
```

```go
import (
	"context"
	"net/http"
	"os"

	"github.com/hookdeck/outpost/sdks/outpost-go/mcpevents"
	"github.com/hookdeck/outpost/sdks/outpost-go/mcpevents/mcpsdk"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

client, err := mcpevents.NewClient(mcpevents.Config{
	ServerURL: "http://localhost:3333/api/v2",
	APIKey:    os.Getenv("OUTPOST_API_KEY"),
})
if err != nil {
	return err
}
handlers, err := mcpevents.NewHandlers(client, mcpevents.Options{
	ResolveTenant: func(ctx context.Context, principal string) (string, error) { return tenantFor(ctx, principal) },
	AllowedTopics: func(ctx context.Context, principal string) ([]string, error) { return topicsFor(ctx, principal) }, // optional
})
if err != nil {
	return err
}

server := mcp.NewServer(&mcp.Implementation{Name: "store", Version: "1.0.0"}, nil)
err = mcpsdk.Register(server, handlers, mcpsdk.Options{
	// The authenticated subject your TokenVerifier set. Never the OAuth client
	// ID: every user of one MCP client shares it.
	ResolvePrincipal: func(ctx context.Context, req mcp.Request) (string, error) {
		if extra := req.GetExtra(); extra != nil && extra.TokenInfo != nil {
			return extra.TokenInfo.UserID, nil
		}
		return "", nil
	},
})
if err != nil {
	return err
}
handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, &mcp.StreamableHTTPOptions{Stateless: true})
http.Handle("/mcp", auth.RequireBearerToken(verifyToken, nil)(handler))
```

- Requests without a principal, or whose principal has no tenant (`ResolveTenant` returns `""`), are rejected with `Forbidden`.
- `AllowedTopics` is your authorization of event types. `events/list` only returns these topics; an empty (or nil) list answers `{"events":[]}` without calling Outpost. `events/subscribe` rejects any other name with `NotFound` (`data: {"kind":"event"}`), the same error as an unknown topic, and also sends the list to Outpost, which enforces it too. `events/unsubscribe` is never filtered, so cleanup always works.
- Outpost's `mcp_error` (HTTP 422) is returned as the JSON-RPC error with its code, message and data. Any other failure (Outpost unreachable, an unexpected status, a resolver error) becomes a generic `Internal error`; set `Options.OnError` to see the details. Return an `*mcpevents.RPCError` from a resolver to answer with your own error.
- `Options.Codes` (`mcpevents.CodesSketch`, the default, or `mcpevents.CodesSEP3415`) picks the numbers of the errors raised locally (`Forbidden`, `NotFound`). Match Outpost's `MCP_ERROR_CODES`; Outpost fills the code of its own errors.

## Lower-level calls

`(*mcpevents.Handlers).HandleList`, `HandleSubscribe` and `HandleUnsubscribe(ctx, principal, params)` (or `Handle(ctx, method, principal, params)`) work with any JSON-RPC layer: they take the raw params and return the MCP result as a `json.RawMessage`, ready to send verbatim, or an `*mcpevents.RPCError` with `Code`, `Message` and `Data`.

`*mcpevents.Client` calls the endpoints directly and returns Outpost's JSON byte for byte:

```go
result, err := client.ListEvents(ctx, "store_123", mcpevents.ListEventsQuery{Cursor: cursor, Topics: []string{"order.created"}}) // GET  /tenants/{tenant_id}/mcp/events
result, err = client.Subscribe(ctx, "store_123", mcpevents.SubscribeRequest{Principal: principal, Params: params})               // PUT  /tenants/{tenant_id}/mcp/subscriptions
var mcpErr *mcpevents.MCPError
if errors.As(err, &mcpErr) {
	// mcpErr.Code, mcpErr.Message and mcpErr.Data are the JSON-RPC error to return.
}
result, err = client.Unsubscribe(ctx, "store_123", mcpevents.UnsubscribeRequest{Principal: principal, Params: params})           // POST /tenants/{tenant_id}/mcp/subscriptions/unsubscribe
```

Generated `Mcp` methods for these and the operator endpoints (listing and revoking subscriptions) arrive with the next SDK regeneration. The generated event list method drops an empty `Topics` slice from the query string, and Outpost then lists every MCP-enabled topic. For a principal allowed no topics, answer `{"events":[]}` yourself, or use the helpers above, which do.

# Development

## Maturity

This SDK is in beta, and there may be breaking changes between versions without a major version update. Therefore, we recommend pinning usage
to a specific package version. This way, you can install the same version each time without breaking changes unless you are intentionally
looking for the latest version.

## Contributions

While we value open-source contributions to this SDK, this library is generated programmatically. Any manual changes added to internal files will be overwritten on the next generation. 
We look forward to hearing your feedback. Feel free to open a PR or an issue with a proof of concept and we'll do our best to include it in a future release. 

### SDK Created by [Speakeasy](https://www.speakeasy.com/?utm_source=github-com/hookdeck/outpost/sdks/outpost-go&utm_campaign=go)
