# Configuration

## Overview

The Configuration API is available for **managed Outpost** deployments only. It allows you to read and update instance-level settings — the same settings available as environment variables in self-hosted deployments.


### Available Operations

* [get_managed_config](#get_managed_config) - Get Managed Configuration
* [update_managed_config](#update_managed_config) - Update Managed Configuration

## get_managed_config

Returns managed Outpost configuration values.

This endpoint is only available for the managed version.
In self-hosted deployments, configuration is controlled through environment variables instead.


### Example Usage

<!-- UsageSnippet language="python" operationID="getManagedConfig" method="get" path="/config" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.configuration.get_managed_config()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.ManagedConfig](../../models/managedconfig.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |

## update_managed_config

Updates one or more managed Outpost configuration values. Null values clear the configuration and reverts to Outpost default behavior.

This endpoint is only available for the managed version.
In self-hosted deployments, configuration is controlled through environment variables instead.

Only the supported configuration keys are accepted.


### Example Usage

<!-- UsageSnippet language="python" operationID="updateManagedConfig" method="patch" path="/config" -->
```python
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.configuration.update_managed_config(request={
        "destinations_webhook_mode": "default",
        "topics": "user.created,user.updated",
    })

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `request`                                                           | [models.ManagedConfig](../../models/managedconfig.md)               | :heavy_check_mark:                                                  | The request object to use for the request.                          |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[models.ManagedConfig](../../models/managedconfig.md)**

### Errors

| Error Type                 | Status Code                | Content Type               |
| -------------------------- | -------------------------- | -------------------------- |
| errors.BadRequestError     | 400                        | application/json           |
| errors.UnauthorizedError   | 401                        | application/json           |
| errors.APIErrorResponse    | 422                        | application/json           |
| errors.InternalServerError | 500                        | application/json           |
| errors.APIError            | 4XX, 5XX                   | \*/\*                      |