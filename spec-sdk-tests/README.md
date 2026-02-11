# Outpost API Contract Tests

This directory contains contract tests for the Outpost API. The tests use a Speakeasy-generated TypeScript SDK to validate the API implementation against the OpenAPI specification.

## Overview

The primary goal of these tests is to ensure that the Outpost API implementation strictly adheres to its OpenAPI contract. This is achieved indirectly by using a TypeScript SDK that is generated directly from the OpenAPI specification (`../apis/openapi.yaml`).

The workflow is as follows:

1.  The OpenAPI specification serves as the single source of truth.
2.  The Speakeasy CLI generates a TypeScript SDK based on this specification.
3.  The test suite is written against the generated SDK.

Because the SDK's models and methods are a direct representation of the OpenAPI spec, any deviation in the API's behavior (such as incorrect response payloads or status codes) will cause the SDK's built-in validation to fail, thus failing the tests.

## Quick Start

The recommended way to run the tests is using the provided script, which ensures the API is healthy before executing the test suite.

```bash
# 1. Ensure all prerequisites are met (see below)

# 2. Generate and build the TypeScript SDK
./scripts/regenerate-sdk.sh

# 3. Install test suite dependencies
npm install

# 4. Ensure an Outpost instance is running and accessible

# 5. Run the test script
./scripts/run-tests.sh
```

## Prerequisites

Before running the tests, ensure you have the following:

1.  **Node.js**: Version 18.0.0 or higher.
2.  **Go**: Required if you plan to run an Outpost instance locally.
3.  **Speakeasy CLI**: Required for regenerating the SDK.
4.  **Running Outpost Instance**: The tests require a running Outpost API server, either locally or on a remote server.
5.  **Environment File**: A `.env` file must be created and configured for the test suite.

## Setting Up an Outpost Instance

These tests must be run against a live Outpost server. You can either run one locally or target a remote instance.

### Option 1: Running a Local Instance

**1. Configure the Outpost Environment:**

From the root of the repository, copy the example environment file:

```bash
cp .env.example .env
```

Ensure the `API_KEY` variable is set in this file. This is the key your local Outpost instance will use.

**2. Start the Outpost Server:**

In a dedicated terminal, run the following command from the repository root:

```bash
go run cmd/outpost/main.go
```

The server should now be running and accessible at `http://localhost:3333`.

### Option 2: Targeting a Remote Instance

If you are running tests against a remote Outpost server, you must configure the `API_BASE_URL` in the test suite's `.env` file to point to your server's address.

**Managed Outpost (e.g. api.outpost.hookdeck.com):** The `/healthz` endpoint is not available on managed Outpost. Set `SKIP_HEALTH_CHECK=true` in your `.env` so the run script skips the health check and proceeds to run tests.

## Test Suite Configuration

The test suite requires its own `.env` file, located within this directory (`spec-sdk-tests`).

**1. Create the `.env` file:**

Start by copying the example file:

```bash
cp .env.example .env
```

**2. Configure Environment Variables:**

The following variables are **mandatory** and must be set in your `.env` file:

- `API_KEY`: The API key for authenticating with the Outpost API. **This key must match the API key configured on the target Outpost instance.**
- `TEST_TOPICS`: A comma-separated list of topics that already exist on your Outpost instance (e.g., `user.created,user.updated,order.created,heartbeat`). The tests will fail if these topics do not exist.

Optional variables:

- `API_BASE_URL`: The base URL of the Outpost API (default: `http://localhost:3333/api/v1`). **Set this if you are targeting a remote instance.**
- `TENANT_ID`: The tenant ID to use for the tests (default: `default`).
- `DEBUG_API_REQUESTS`: Set to `true` to enable detailed request logging (default: `false`).
- `TEST_DELAY_MS`: Delay in milliseconds before each test (default: `0`). Set to e.g. `50` to reduce 429 (Too Many Requests) when running against rate-limited APIs.

## SDK Regeneration

If you make changes to the OpenAPI specification (`../apis/openapi.yaml`), you must regenerate the TypeScript SDK to ensure the tests are validating against the latest contract.

The `regenerate-sdk.sh` script handles this process automatically:

```bash
./scripts/regenerate-sdk.sh
```

This script will:

1.  Navigate to the SDK directory (`/sdks/outpost-typescript`).
2.  Run `speakeasy run` to regenerate the SDK files.
3.  Run `npm run build` to compile the new SDK code.

After regenerating, you may need to update the tests if there are breaking changes in the spec.

## NPM Scripts

The following scripts are available to run, lint, and format the tests:

| Script                    | Description                                                    |
| ------------------------- | -------------------------------------------------------------- |
| `npm test`                | Runs the full validation test suite.                           |
| `npm run test:validation` | Runs the mocha test suite directly.                            |
| `npm run test:watch`      | Runs tests in watch mode, re-running on file changes.          |
| `npm run test:coverage`   | Generates a test coverage report.                              |
| `npm run lint:spec`       | Lints the OpenAPI specification file (`../apis/openapi.yaml`). |
| `npm run validate:spec`   | Validates the syntax of the OpenAPI specification.             |
| `npm run format`          | Formats all TypeScript files using Prettier.                   |
| `npm run format:check`    | Checks for formatting issues without modifying files.          |
| `npm run type-check`      | Runs TypeScript type-checking without compiling.               |
| `npm run test:failures`   | Runs only the tests that were previously failing (Events + Topics). |

### Debugging API responses (curl)

To see raw API responses and distinguish SDK vs API vs parameter issues, use the curl script. It performs the same requests as the failing Events tests (per the OpenAPI spec: Bearer auth, GET /events with query params, GET /events/{event_id}).

```bash
# From spec-sdk-tests (uses .env: API_BASE_URL, API_KEY, TENANT_ID)
./scripts/curl-events.sh

# With destination filter (reproduces the 500 from tests)
DESTINATION_ID=des_xxx ./scripts/curl-events.sh
```

Requires `jq` for readable JSON (optional; script still prints body without it).

## Test Structure

The tests are organized by resource type within the `tests/` directory.

```
tests/
├── destinations/
│   ├── gcp-pubsub.test.ts    # GCP Pub/Sub destination tests
│   └── ...                   # Other destination types
└── utils/
    └── sdk-client.ts         # SDK client wrapper
```

## Writing Tests

When adding new tests, please adhere to the following guidelines:

1.  **Use the SDK Client**: Import and use the shared SDK client from `utils/sdk-client.ts`.
2.  **Cover All Scenarios**: Test both happy paths and error conditions.
3.  **Validate Responses**: Ensure response structures conform to the OpenAPI specification. The SDK performs automatic validation, but explicit assertions are encouraged.
4.  **Be Thorough**: Test all CRUD (Create, Read, Update, Delete) operations for each resource.
