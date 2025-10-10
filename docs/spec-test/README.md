# Outpost OpenAPI Contract Testing

This directory contains contract tests for the Outpost API using Prism to validate against the OpenAPI specification.

## Quick Start

```bash
# 1. Install dependencies
npm install

# 2. Start Outpost API (in another terminal)
cd ../.. && go run cmd/outpost/main.go

# 3. Run tests with Prism validation
./scripts/run-tests.sh
```

For detailed instructions, see [Testing Guide](./TESTING_GUIDE.md).

## Overview

The test suite validates that the Outpost API implementation conforms to the OpenAPI specification defined in `docs/apis/openapi.yaml`. It uses Prism as a validating proxy to intercept API calls and validate both requests and responses against the spec.

## Prerequisites

- Node.js >= 18.0.0
- Running Outpost instance on `http://localhost:3333`

## Installation

```bash
npm install
```

## Running Tests

### Automated (Recommended)

```bash
# This script checks API health, starts Prism proxy if needed, runs tests, and cleans up
./scripts/run-tests.sh
```

### Manual Execution

**Terminal 1 - Start Prism proxy:**

```bash
npm run prism:proxy
```

**Terminal 2 - Run tests:**

```bash
npm test
```

## Test Structure

```
tests/
├── destinations/
│   ├── gcp-pubsub.test.ts    # GCP Pub/Sub destination tests
│   └── ...                    # Other destination types
└── utils/
    └── api-client.ts          # API client with Prism support
```

## NPM Scripts

| Script                   | Purpose                      |
| ------------------------ | ---------------------------- |
| `npm test`               | Run all contract tests       |
| `npm run test:watch`     | Run tests in watch mode      |
| `npm run test:coverage`  | Generate coverage reports    |
| `npm run prism:proxy`    | Start Prism in proxy mode    |
| `npm run prism:mock`     | Start Prism mock server      |
| `npm run prism:validate` | Validate OpenAPI spec        |
| `npm run lint:spec`      | Lint OpenAPI specification   |
| `npm run validate:spec`  | Validate OpenAPI syntax      |
| `npm run format`         | Format TypeScript files      |
| `npm run type-check`     | Run TypeScript type checking |

## Configuration

**Important:** You must configure an API key before running tests.

1. Copy `.env.example` to `.env`:

```bash
cp .env.example .env
```

2. **Set the API_KEY in `.env`:**

```bash
# API Authentication (REQUIRED)
API_KEY=your-api-key-here
```

This API key must match the `API_KEY` environment variable configured in your Outpost server instance.

### Environment Variables

Tests can be configured via environment variables:

- `API_KEY`: **Required** - API key for authenticating with Outpost (must match server config)
- `TEST_TOPICS`: **Required** - Comma-separated list of topics that exist on your Outpost instance (e.g., `user.created,user.updated,user.deleted`)
- `API_BASE_URL`: Prism proxy URL (default: `http://localhost:9000`)
- `API_DIRECT_URL`: Direct API URL for setup/teardown (default: `http://localhost:3333`)
- `TENANT_ID`: Tenant ID for tests (default: `default`)
- `DEBUG_API_REQUESTS`: Enable request logging (default: `false`)

Create a `.env` file based on `.env.example`:

```bash
cp .env.example .env
```

**Important:** You must configure `TEST_TOPICS` with topics that already exist on your Outpost backend. The tests will fail if these topics don't exist, as the backend validates topic existence when creating destinations.

## Writing Tests

Tests should:

1. Use the API client from `utils/api-client.ts`
2. Point to the Prism proxy (port 9000) for validation
3. Test both happy paths and error scenarios
4. Validate response structures match the OpenAPI spec
5. Test all CRUD operations for each destination type

Example:

```typescript
import { describe, it } from 'mocha';
import { expect } from 'chai';
import { createProxyClient } from '../utils/api-client';

describe('GCP Pub/Sub Destinations', () => {
  const client = createProxyClient();

  it('should create a GCP Pub/Sub destination', async () => {
    const destination = await client.createDestination({
      type: 'gcp_pubsub',
      topics: '*',
      config: {
        project_id: 'test-project',
        topic: 'test-topic',
      },
      credentials: {
        service_account_json: '{}',
      },
    });

    expect(destination).to.have.property('id');
    expect(destination.type).to.equal('gcp_pubsub');
  });
});
```

## Troubleshooting

### Prism proxy not starting

Ensure Node.js is installed and the port is available:

```bash
lsof -i :9000
```

### Tests timing out

Increase timeout in mocha configuration or specific tests:

```typescript
it('should handle long operation', async function () {
  this.timeout(30000); // 30 seconds
  // test code
});
```

### Validation failures

Check that:

1. The OpenAPI spec is valid: `npm run validate:spec`
2. The API implementation matches the spec
3. Request/response payloads conform to schema definitions
