# Contributing Documentation Plan

## Overview

Create comprehensive documentation to help developers understand the test suite architecture, add new tests, and contribute improvements to the OpenAPI validation project.

## Goals

1. Document the test suite architecture and design patterns
2. Provide clear guidelines for adding new tests
3. Explain the factory pattern and test utilities
4. Define development workflow and best practices
5. Make testing accessible to new contributors
6. Reduce onboarding time for test development

## Requirements

### Documentation Locations

1. **`CONTRIBUTING.md`** (root) - Add testing section
2. **`spec-sdk-tests/README.md`** - Update with comprehensive guide
3. **`spec-sdk-tests/DEVELOPMENT.md`** - New developer guide
4. **Code comments** - Inline documentation in factories and utilities

### Content Coverage

- Architecture overview
- Factory pattern explanation
- How to run tests locally
- How to add new destination type tests
- How to add tests for new API endpoints
- Debugging failed tests
- CI/CD integration overview
- Coverage requirements

## Technical Approach

### 1. Update Root CONTRIBUTING.md

Add new section after existing content:

```markdown
## Testing

Outpost includes comprehensive OpenAPI validation tests to ensure API endpoints match their specification.

### Test Suite Overview

The test suite is located in [`spec-sdk-tests/`](./spec-sdk-tests/) and uses:
- **TypeScript** with Jest for test framework
- **Factory pattern** for test data creation
- **SDK client** wrapper for API interactions
- **147 tests** covering 8 destination types

### Running Tests Locally

```bash
# Navigate to test directory
cd spec-sdk-tests

# Install dependencies
npm install

# Set up environment
export OUTPOST_BASE_URL=http://localhost:8080

# Run Outpost locally (in separate terminal)
cd ..
go run cmd/outpost/main.go serve

# Run all tests
npm test

# Run tests for specific destination
npm test tests/destinations/webhook.test.ts

# Run tests in watch mode
npm test -- --watch
```

### Test Architecture

#### Factory Pattern

Tests use factories to create consistent test data:

```typescript
import { createTenant } from '../factories/tenant.factory';
import { createDestination } from '../factories/destination.factory';

// Create tenant
const tenant = await createTenant(client);

// Create destination with factory
const destination = await createDestination(client, tenant.id, {
  type: 'webhook',
  url: 'https://example.com/webhook'
});
```

**Available Factories:**
- [`tenant.factory.ts`](./spec-sdk-tests/factories/tenant.factory.ts) - Tenant creation
- [`destination.factory.ts`](./spec-sdk-tests/factories/destination.factory.ts) - All destination types
- [`event.factory.ts`](./spec-sdk-tests/factories/event.factory.ts) - Event publishing

#### SDK Client Wrapper

The [`sdk-client.ts`](./spec-sdk-tests/utils/sdk-client.ts) wrapper provides:
- Authentication token management
- Tenant context handling
- Consistent error handling
- Type-safe API calls

### Adding Tests for New Endpoints

When adding a new API endpoint to Outpost:

1. **Update OpenAPI spec** (`docs/apis/openapi.yaml`)
2. **Add endpoint to SDK** (handled by Speakeasy generation)
3. **Create tests** following this pattern:

```typescript
describe('New Feature API', () => {
  let client: SDKClient;
  let tenant: Tenant;

  beforeEach(async () => {
    client = new SDKClient(process.env.OUTPOST_BASE_URL!);
    tenant = await createTenant(client);
  });

  afterEach(async () => {
    await client.sdk.tenants.delete(tenant.id);
  });

  describe('POST /tenants/{tenant_id}/feature', () => {
    it('should create a new feature', async () => {
      const response = await client.sdk.features.create({
        tenantId: tenant.id,
        requestBody: {
          name: 'test-feature',
          config: { key: 'value' }
        }
      });

      expect(response.statusCode).toBe(201);
      expect(response.feature?.name).toBe('test-feature');
    });

    it('should validate required fields', async () => {
      await expect(
        client.sdk.features.create({
          tenantId: tenant.id,
          requestBody: {} // Missing required fields
        })
      ).rejects.toThrow();
    });
  });
});
```

4. **Run tests** to verify
5. **Update coverage** - Tests should maintain 85%+ endpoint coverage

### Adding Tests for New Destination Types

To add tests for a new destination type (e.g., `kafka`):

1. **Create test file**: `spec-sdk-tests/tests/destinations/kafka.test.ts`

```typescript
import { SDKClient } from '../../utils/sdk-client';
import { createTenant } from '../../factories/tenant.factory';
import { createDestination } from '../../factories/destination.factory';
import { publishEvent } from '../../factories/event.factory';

describe('Kafka Destination', () => {
  let client: SDKClient;
  let tenant: Tenant;

  beforeEach(async () => {
    client = new SDKClient(process.env.OUTPOST_BASE_URL!);
    tenant = await createTenant(client);
  });

  afterEach(async () => {
    await client.sdk.tenants.delete(tenant.id);
  });

  describe('Configuration', () => {
    it('should create Kafka destination with valid config', async () => {
      const destination = await createDestination(client, tenant.id, {
        type: 'kafka',
        name: 'kafka-dest',
        config: {
          brokers: ['localhost:9092'],
          topic: 'events',
          sasl_mechanism: 'PLAIN',
          sasl_username: 'user',
          sasl_password: 'pass'
        }
      });

      expect(destination.type).toBe('kafka');
      expect(destination.config.brokers).toEqual(['localhost:9092']);
    });

    it('should validate required configuration', async () => {
      await expect(
        createDestination(client, tenant.id, {
          type: 'kafka',
          config: {} // Missing required fields
        })
      ).rejects.toThrow();
    });
  });

  describe('Event Delivery', () => {
    it('should deliver events to Kafka', async () => {
      const destination = await createDestination(client, tenant.id, {
        type: 'kafka',
        config: {
          brokers: ['localhost:9092'],
          topic: 'test-topic'
        }
      });

      const event = await publishEvent(client, tenant.id, {
        topic: destination.topic_id,
        data: { message: 'test' }
      });

      expect(event.status).toBe('delivered');
    });
  });
});
```

2. **Add factory support** in `destination.factory.ts`:

```typescript
export interface KafkaConfig {
  brokers: string[];
  topic: string;
  sasl_mechanism?: string;
  sasl_username?: string;
  sasl_password?: string;
}

// Add to createDestination function
case 'kafka':
  return {
    name: options.name || 'kafka-destination',
    type: 'kafka',
    config: options.config || {
      brokers: ['localhost:9092'],
      topic: 'events'
    }
  };
```

3. **Run and verify tests**:

```bash
npm test tests/destinations/kafka.test.ts
```

### Debugging Failed Tests

#### View Test Output

```bash
# Verbose output
npm test -- --verbose

# Show console logs
npm test -- --silent=false
```

#### Common Issues

**Connection Refused:**
```
Error: connect ECONNREFUSED 127.0.0.1:8080
```
→ Ensure Outpost is running: `go run cmd/outpost/main.go serve`

**Authentication Failed:**
```
Error: Unauthorized
```
→ Check tenant creation and token handling in SDK client

**Test Timeout:**
```
Error: Timeout - Async callback was not invoked within the 5000 ms timeout
```
→ Increase timeout: `jest.setTimeout(30000);` or check if service is responding

#### Debug Individual Tests

```bash
# Run single test file
npm test webhook.test.ts

# Run specific test case
npm test -- -t "should create webhook destination"

# Run in debug mode (VS Code)
# Add breakpoints and use "Jest: Debug" configuration
```

### Test Best Practices

1. **Use factories** - Don't create test data manually
2. **Clean up** - Always delete resources in `afterEach`
3. **Test edge cases** - Invalid inputs, missing fields, boundary conditions
4. **Descriptive names** - Test names should explain what is being tested
5. **Arrange-Act-Assert** - Structure tests clearly
6. **Avoid flakiness** - Don't rely on timing, use proper async/await
7. **Test isolation** - Each test should be independent

### Test Coverage Requirements

- **Minimum coverage**: 85% of OpenAPI endpoints
- **Coverage check**: Runs automatically in CI/CD
- **View coverage**: `npm run coverage:generate`

See [`TEST_STATUS.md`](./spec-sdk-tests/TEST_STATUS.md) for current coverage.

### CI/CD Integration

Tests run automatically on:
- Pull requests (all tests)
- Commits to main/develop (all tests)
- Nightly scheduled runs (full suite)

See [`.github/workflows/openapi-validation-tests.yml`](./.github/workflows/openapi-validation-tests.yml)

### Additional Resources

- [Test Suite README](./spec-sdk-tests/README.md)
- [Test Status Report](./spec-sdk-tests/TEST_STATUS.md)
- [OpenAPI Specification](./docs/apis/openapi.yaml)
- [Development Guide](./spec-sdk-tests/DEVELOPMENT.md)
```

### 2. Create spec-sdk-tests/DEVELOPMENT.md

This will be a comprehensive developer guide covering architecture, patterns, workflows, and troubleshooting. The document should be approximately 750 lines and include:

- Directory structure overview
- Design patterns (Factory, SDK Wrapper)
- Development workflow (setup, writing tests, debugging)
- Test data management strategies
- Advanced topics (custom matchers, parameterized tests, retry logic)
- Performance optimization tips
- Contributing checklist

## Acceptance Criteria

- [ ] Root `CONTRIBUTING.md` has comprehensive testing section
- [ ] `spec-sdk-tests/README.md` updated with developer guide
- [ ] New `spec-sdk-tests/DEVELOPMENT.md` created
- [ ] Factory pattern explained with examples
- [ ] Test writing workflow documented step-by-step
- [ ] Debugging guide with common issues and solutions
- [ ] Code examples are accurate and tested
- [ ] Links to related documentation included
- [ ] Clear guidance for adding new destination types
- [ ] Clear guidance for adding tests for new endpoints
- [ ] Best practices and anti-patterns documented

## Dependencies

None (can be implemented independently)

## Risks & Considerations

1. **Documentation Drift**
   - Risk: Documentation becomes outdated as code evolves
   - Mitigation: Include docs updates in PR checklist, automated checks

2. **Example Accuracy**
   - Risk: Code examples may contain errors
   - Mitigation: Extract examples from working tests, validate during build

3. **Overwhelming Detail**
   - Risk: Too much documentation overwhelms new contributors
   - Mitigation: Layer information (quick start → detailed guide → advanced topics)

4. **Maintenance Burden**
   - Risk: Multiple documentation files need updates
   - Mitigation: Use links to single source of truth, avoid duplication

## Future Enhancements

- Video tutorials for visual learners
- Interactive code playground for testing
- Auto-generated API documentation from OpenAPI spec
- FAQ section based on common questions
- Contributing guide for non-code contributions (docs, examples)

---

**Estimated Effort**: 2-3 days  
**Priority**: Medium  
**Dependencies**: None (can start immediately)