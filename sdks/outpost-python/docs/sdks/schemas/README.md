# Schemas
(*schemas*)

## Overview

Operations for retrieving destination type schemas.

### Available Operations

* [list_tenant_destination_types](#list_tenant_destination_types) - List Destination Type Schemas (for Tenant)
* [get](#get) - Get Destination Type Schema (for Tenant)
* [list_destination_types_jwt](#list_destination_types_jwt) - List Destination Type Schemas (JWT Auth)
* [get_destination_type_jwt](#get_destination_type_jwt) - Get Destination Type Schema

## list_tenant_destination_types

Returns a list of JSON-based input schemas for each available destination type. Requires Admin API Key or Tenant JWT.

### Example Usage

<!-- UsageSnippet language="python" operationID="listTenantDestinationTypeSchemas" method="get" path="/{tenant_id}/destination-types" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.schemas.list_tenant_destination_types()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                             | Type                                                                  | Required                                                              | Description                                                           |
| --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- | --------------------------------------------------------------------- |
| `tenant_id`                                                           | *Optional[str]*                                                       | :heavy_minus_sign:                                                    | The ID of the tenant. Required when using AdminApiKey authentication. |
| `retries`                                                             | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)      | :heavy_minus_sign:                                                    | Configuration to override the default retry behavior of the client.   |

### Response

**[List[models.DestinationTypeSchema]](../../models/.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## get

Returns the input schema for a specific destination type. Requires Admin API Key or Tenant JWT.

### Example Usage

<!-- UsageSnippet language="python" operationID="getTenantDestinationTypeSchema" method="get" path="/{tenant_id}/destination-types/{type}" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    tenant_id="<id>",
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.schemas.get(type_=models.GetTenantDestinationTypeSchemaType.HOOKDECK)

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                                       | Type                                                                                            | Required                                                                                        | Description                                                                                     |
| ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- |
| `type`                                                                                          | [models.GetTenantDestinationTypeSchemaType](../../models/gettenantdestinationtypeschematype.md) | :heavy_check_mark:                                                                              | The type of the destination.                                                                    |
| `tenant_id`                                                                                     | *Optional[str]*                                                                                 | :heavy_minus_sign:                                                                              | The ID of the tenant. Required when using AdminApiKey authentication.                           |
| `retries`                                                                                       | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                                | :heavy_minus_sign:                                                                              | Configuration to override the default retry behavior of the client.                             |

### Response

**[models.DestinationTypeSchema](../../models/destinationtypeschema.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 401, 403, 407                | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## list_destination_types_jwt

Returns a list of JSON-based input schemas for each available destination type (infers tenant from JWT).

### Example Usage

<!-- UsageSnippet language="python" operationID="listDestinationTypeSchemasJwt" method="get" path="/destination-types" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.schemas.list_destination_types_jwt()

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                           | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `retries`                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)    | :heavy_minus_sign:                                                  | Configuration to override the default retry behavior of the client. |

### Response

**[List[models.DestinationTypeSchema]](../../models/.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.NotFoundError         | 404                          | application/json             |
| errors.UnauthorizedError     | 403, 407                     | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |

## get_destination_type_jwt

Returns the input schema for a specific destination type.

### Example Usage

<!-- UsageSnippet language="python" operationID="getDestinationTypeSchema" method="get" path="/destination-types/{type}" -->
```python
from outpost_sdk import Outpost, models


with Outpost(
    security=models.Security(
        admin_api_key="<YOUR_BEARER_TOKEN_HERE>",
    ),
) as outpost:

    res = outpost.schemas.get_destination_type_jwt(type_=models.GetDestinationTypeSchemaType.RABBITMQ)

    # Handle response
    print(res)

```

### Parameters

| Parameter                                                                           | Type                                                                                | Required                                                                            | Description                                                                         |
| ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------- |
| `type`                                                                              | [models.GetDestinationTypeSchemaType](../../models/getdestinationtypeschematype.md) | :heavy_check_mark:                                                                  | The type of the destination.                                                        |
| `retries`                                                                           | [Optional[utils.RetryConfig]](../../models/utils/retryconfig.md)                    | :heavy_minus_sign:                                                                  | Configuration to override the default retry behavior of the client.                 |

### Response

**[models.DestinationTypeSchema](../../models/destinationtypeschema.md)**

### Errors

| Error Type                   | Status Code                  | Content Type                 |
| ---------------------------- | ---------------------------- | ---------------------------- |
| errors.UnauthorizedError     | 403, 407                     | application/json             |
| errors.TimeoutErrorT         | 408                          | application/json             |
| errors.RateLimitedError      | 429                          | application/json             |
| errors.BadRequestError       | 400, 413, 414, 415, 422, 431 | application/json             |
| errors.TimeoutErrorT         | 504                          | application/json             |
| errors.NotFoundError         | 501, 505                     | application/json             |
| errors.InternalServerError   | 500, 502, 503, 506, 507, 508 | application/json             |
| errors.BadRequestError       | 510                          | application/json             |
| errors.UnauthorizedError     | 511                          | application/json             |
| errors.APIError              | 4XX, 5XX                     | \*/\*                        |