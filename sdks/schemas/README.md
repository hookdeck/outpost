# SDK schemas and overlays

This directory contains the OpenAPI schema and Speakeasy overlays used to generate the Outpost SDKs (Go, TypeScript, Python).

## Authentication: single "API key" in the SDK

Outpost’s API supports two authentication mechanisms:

1. **Admin API key** — static key (e.g. from `API_KEY`), used for admin operations (publish, manage tenants, get tenant token).
2. **JWT (tenant token)** — obtained via `GET /tenants/{tenant_id}/token` (requires Admin API key). Scoped to one tenant, time-limited; used for tenant-scoped operations (e.g. list destinations for that tenant).

For the SDKs we **collapse these into a single authentication surface**: one parameter, `apiKey` (or language equivalent). The **value** of that parameter can be either the Admin API key or the JWT. So SDK users only deal with “API key”; they pass the admin key or the token from `tenants.getToken()` as that same parameter.

The file **`security-collapse-overlay.yaml`** does this: it replaces the two OpenAPI security schemes (AdminApiKey and TenantJwt) with a single Bearer scheme named `apiKey` for SDK generation. The base OpenAPI spec keeps both schemes for API documentation; the overlay is applied during SDK generation so the generated clients expose a single auth option.

## Other overlays

- **`speakeasy-modifications-overlay.yaml`** — operation naming and other Speakeasy tweaks.
- **`pagination-fixes-overlay.yaml`** — pagination behavior.
- **`error-types.yaml`** — error response typing.

## Regenerating SDKs

From the repo root, use the Speakeasy workflow (e.g. `spec-sdk-tests/scripts/regenerate-sdk.sh` or `speakeasy run` per SDK). The overlays are applied when generating from the schema.

## Troubleshooting: auth example returns 401 when using tenant token

**What action is failing?**  
The auth example calls **GET /tenants/{tenant_id}/destinations** (list destinations for a tenant) with the **tenant JWT** as the Bearer token (the value from `tenants.getToken()`). So the request is: list destinations for tenant T, authenticated with the JWT that was issued for tenant T.

**Is that allowed?**  
Yes. The server allows two credentials: (1) Admin API key — full permissions. (2) JWT — permissions only for the tenant in the JWT’s `sub` claim. List destinations is a tenant-scoped operation (not AdminOnly). The route has `:tenant_id` in the path; the server requires that the JWT’s `sub` equals that path param. The example uses the same tenant ID for both getToken and list, so the request is authorized by design.

**Why would the server return 401?**  
The middleware (`internal/apirouter/auth_middleware.go`) returns 401 only in two cases:

1. **Missing or invalid Authorization header** — e.g. no `Bearer` prefix or empty token.
2. **JWT verification failed** — after checking the token is not the admin API key, the server calls `JWT.Extract(jwtSecret, token)`. If that fails (invalid signature, wrong/empty `API_JWT_SECRET`, expired, or malformed token), the server returns 401.

So 401 when using the tenant token means the server could not accept the credential: either the header wasn’t sent correctly, or the JWT could not be verified. The most likely cause is **`API_JWT_SECRET` is not set or not the same** on the deployment that validates the request as on the one that issued the token (GET /tenants/…/token uses the same secret to sign). For a single instance, both issue and validate use the same config; for multiple instances or a proxy, every instance that validates must have the same `API_JWT_SECRET`.

**What to do:**  
- Ensure `API_JWT_SECRET` is set on the Outpost API deployment and is identical for all processes that issue or validate tenant tokens.  
- To confirm the examples and SDK behave correctly, run the auth example against a **local** Outpost with `API_JWT_SECRET` set; the server test `"jwt returns destinations on own tenant"` in `destination_handlers_test.go` shows the same flow succeeding.
