# CI/CD Integration Plan

## Overview

Integrate the OpenAPI validation test suite into GitHub Actions to automatically validate API endpoints on every pull request and commit to main branches.

## Goals

1. Automate test execution in CI/CD pipeline
2. Prevent regressions in API functionality
3. Provide rapid feedback to developers
4. Display test status in README badges
5. Alert on test failures

## Requirements

### Test Environment Setup

The CI environment must:
- Run Outpost instance with all dependencies (Redis, PostgreSQL)
- Support all 8 destination types (including AWS, Azure, GCP services)
- Use Docker Compose for orchestration
- Support test mode without external service dependencies
- Complete setup in < 5 minutes

### Test Execution Strategy

**When to Run:**
- On every pull request (all tests)
- On push to `main` branch (all tests)
- On push to `develop` branch (all tests)
- Scheduled nightly runs (full suite with external services)
- Manual trigger option for debugging

**Test Organization:**
- Run all 147 tests by default
- Support test filtering by destination type
- Parallel execution where possible
- Fail fast on critical errors

## Technical Approach

### 1. Workflow File Structure

**Location:** `.github/workflows/openapi-validation-tests.yml`

```yaml
name: OpenAPI Validation Tests

on:
  pull_request:
    paths:
      - 'internal/services/api/**'
      - 'docs/apis/openapi.yaml'
      - 'spec-sdk-tests/**'
  push:
    branches:
      - main
      - develop
  schedule:
    # Run nightly at 2 AM UTC
    - cron: '0 2 * * *'
  workflow_dispatch:
    inputs:
      destination_type:
        description: 'Destination type to test (or "all")'
        required: false
        default: 'all'
        type: choice
        options:
          - all
          - webhook
          - aws-sqs
          - rabbitmq
          - azure-servicebus
          - aws-s3
          - hookdeck
          - aws-kinesis
          - gcp-pubsub

jobs:
  test:
    name: Run OpenAPI Tests
    runs-on: ubuntu-latest
    timeout-minutes: 20

    services:
      redis:
        image: redis:7-alpine
        ports:
          - 6379:6379
        options: >-
          --health-cmd "redis-cli ping"
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

      postgres:
        image: postgres:15-alpine
        env:
          POSTGRES_DB: outpost_test
          POSTGRES_USER: outpost
          POSTGRES_PASSWORD: outpost_test_password
        ports:
          - 5432:5432
        options: >-
          --health-cmd pg_isready
          --health-interval 10s
          --health-timeout 5s
          --health-retries 5

    steps:
      - name: Checkout repository
        uses: actions/checkout@v4

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version-file: 'go.mod'
          cache: true

      - name: Set up Node.js
        uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: 'npm'
          cache-dependency-path: 'spec-sdk-tests/package-lock.json'

      - name: Install test dependencies
        working-directory: spec-sdk-tests
        run: npm ci

      - name: Build Outpost
        run: go build -o outpost cmd/outpost/main.go

      - name: Set up test environment
        run: |
          cp .env.test .env
          # Add test-specific configuration
          echo "OUTPOST_TEST_MODE=true" >> .env
          echo "OUTPOST_PORT=8080" >> .env
          echo "REDIS_URL=redis://localhost:6379" >> .env
          echo "DATABASE_URL=postgres://outpost:outpost_test_password@localhost:5432/outpost_test?sslmode=disable" >> .env

      - name: Run database migrations
        run: ./outpost migrate up

      - name: Start Outpost in background
        run: |
          ./outpost serve &
          echo $! > outpost.pid
          # Wait for Outpost to be ready
          timeout 30 bash -c 'until curl -f http://localhost:8080/health; do sleep 1; done'

      - name: Run OpenAPI validation tests
        working-directory: spec-sdk-tests
        env:
          OUTPOST_BASE_URL: http://localhost:8080
          DESTINATION_TYPE: ${{ github.event.inputs.destination_type || 'all' }}
        run: |
          if [ "$DESTINATION_TYPE" = "all" ]; then
            npm test
          else
            npm test -- tests/destinations/${DESTINATION_TYPE}.test.ts
          fi

      - name: Upload test results
        if: always()
        uses: actions/upload-artifact@v4
        with:
          name: test-results
          path: |
            spec-sdk-tests/test-results/
            spec-sdk-tests/coverage/
          retention-days: 30

      - name: Generate test summary
        if: always()
        working-directory: spec-sdk-tests
        run: |
          echo "## OpenAPI Validation Test Results" >> $GITHUB_STEP_SUMMARY
          npm run test:summary >> $GITHUB_STEP_SUMMARY

      - name: Stop Outpost
        if: always()
        run: |
          if [ -f outpost.pid ]; then
            kill $(cat outpost.pid) || true
          fi

      - name: Comment PR with results
        if: github.event_name == 'pull_request' && always()
        uses: actions/github-script@v7
        with:
          script: |
            const fs = require('fs');
            const testSummary = fs.readFileSync('spec-sdk-tests/test-results/summary.md', 'utf8');
            
            github.rest.issues.createComment({
              issue_number: context.issue.number,
              owner: context.repo.owner,
              repo: context.repo.repo,
              body: `## OpenAPI Validation Test Results\n\n${testSummary}`
            });
```

### 2. Docker Compose Setup (Alternative Approach)

**Location:** `.github/workflows/openapi-validation-docker.yml`

```yaml
name: OpenAPI Tests (Docker)

on:
  pull_request:
  push:
    branches: [main, develop]

jobs:
  test:
    runs-on: ubuntu-latest
    
    steps:
      - uses: actions/checkout@v4
      
      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v3
      
      - name: Start test environment
        run: docker compose -f build/test/compose.yml up -d
      
      - name: Wait for services
        run: |
          timeout 60 bash -c 'until curl -f http://localhost:8080/health; do sleep 2; done'
      
      - name: Run tests
        run: docker compose -f build/test/compose.yml exec -T test npm test
      
      - name: Stop environment
        if: always()
        run: docker compose -f build/test/compose.yml down -v
```

### 3. Required Secrets and Environment Variables

**Repository Secrets (Optional for external services):**
```
AWS_ACCESS_KEY_ID          # For AWS SQS/S3/Kinesis tests
AWS_SECRET_ACCESS_KEY
AWS_REGION

AZURE_SERVICE_BUS_CONNECTION_STRING  # For Azure Service Bus tests

GCP_PROJECT_ID             # For GCP Pub/Sub tests
GCP_CREDENTIALS_JSON

RABBITMQ_URL               # For RabbitMQ tests (if using external instance)
```

**Environment Variables (Set in workflow):**
```
OUTPOST_BASE_URL=http://localhost:8080
OUTPOST_TEST_MODE=true     # Enables mock mode for external services
TEST_TIMEOUT=30000         # 30 second timeout per test
NODE_ENV=test
```

### 4. Badge Integration

Add to main `README.md`:

```markdown
[![OpenAPI Tests](https://github.com/hookdeck/outpost/actions/workflows/openapi-validation-tests.yml/badge.svg)](https://github.com/hookdeck/outpost/actions/workflows/openapi-validation-tests.yml)
```

Add detailed badge to `spec-sdk-tests/README.md`:

```markdown
## Test Status

[![OpenAPI Tests](https://github.com/hookdeck/outpost/actions/workflows/openapi-validation-tests.yml/badge.svg)](https://github.com/hookdeck/outpost/actions/workflows/openapi-validation-tests.yml)
[![Test Coverage](https://img.shields.io/badge/coverage-87.8%25-green.svg)](./TEST_STATUS.md)
[![Endpoints Tested](https://img.shields.io/badge/endpoints-147%2F167-yellow.svg)](./TEST_STATUS.md)
```

### 5. Failure Notification Strategy

**Slack Integration (Optional):**

```yaml
      - name: Notify Slack on failure
        if: failure() && github.ref == 'refs/heads/main'
        uses: slackapi/slack-github-action@v1.25.0
        with:
          channel-id: 'engineering-alerts'
          slack-message: |
            :x: OpenAPI validation tests failed on main branch
            Commit: ${{ github.sha }}
            Author: ${{ github.actor }}
            Details: ${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }}
        env:
          SLACK_BOT_TOKEN: ${{ secrets.SLACK_BOT_TOKEN }}
```

**GitHub Issues (Auto-create on failure):**

```yaml
      - name: Create issue on failure
        if: failure() && github.ref == 'refs/heads/main'
        uses: actions/github-script@v7
        with:
          script: |
            await github.rest.issues.create({
              owner: context.repo.owner,
              repo: context.repo.repo,
              title: `OpenAPI Tests Failed - ${new Date().toISOString().split('T')[0]}`,
              body: `The OpenAPI validation tests failed on main branch.\n\nRun: ${context.serverUrl}/${context.repo.owner}/${context.repo.repo}/actions/runs/${context.runId}`,
              labels: ['bug', 'tests', 'automated']
            });
```

## Test Suite Enhancements

### Package.json Scripts

Add to `spec-sdk-tests/package.json`:

```json
{
  "scripts": {
    "test": "jest",
    "test:ci": "jest --ci --coverage --maxWorkers=2",
    "test:summary": "node scripts/generate-summary.js",
    "test:destination": "jest tests/destinations/${DESTINATION_TYPE}.test.ts"
  }
}
```

### Jest Configuration for CI

Update `spec-sdk-tests/jest.config.js`:

```javascript
module.exports = {
  preset: 'ts-jest',
  testEnvironment: 'node',
  testTimeout: 30000,
  reporters: [
    'default',
    ['jest-junit', {
      outputDirectory: './test-results',
      outputName: 'junit.xml',
      classNameTemplate: '{classname}',
      titleTemplate: '{title}',
      ancestorSeparator: ' › ',
      usePathForSuiteName: true
    }],
    ['jest-html-reporter', {
      pageTitle: 'OpenAPI Validation Test Results',
      outputPath: './test-results/index.html',
      includeFailureMsg: true,
      includeConsoleLog: true
    }]
  ],
  collectCoverageFrom: [
    'factories/**/*.ts',
    'utils/**/*.ts',
    'tests/**/*.ts'
  ],
  coverageReporters: ['text', 'lcov', 'html', 'json-summary']
};
```

## Docker Compose Test Configuration

Create `build/test/compose.yml`:

```yaml
version: '3.8'

services:
  postgres:
    image: postgres:15-alpine
    environment:
      POSTGRES_DB: outpost_test
      POSTGRES_USER: outpost
      POSTGRES_PASSWORD: outpost_test
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U outpost"]
      interval: 5s
      timeout: 5s
      retries: 5

  redis:
    image: redis:7-alpine
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 5s
      retries: 5

  outpost:
    build:
      context: ../..
      dockerfile: build/dev/Dockerfile
    ports:
      - "8080:8080"
    environment:
      OUTPOST_TEST_MODE: "true"
      REDIS_URL: redis://redis:6379
      DATABASE_URL: postgres://outpost:outpost_test@postgres:5432/outpost_test?sslmode=disable
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8080/health"]
      interval: 5s
      timeout: 5s
      retries: 10

  test:
    build:
      context: ../..
      dockerfile: build/test/Dockerfile.test
    working_dir: /app/spec-sdk-tests
    environment:
      OUTPOST_BASE_URL: http://outpost:8080
      NODE_ENV: test
    depends_on:
      outpost:
        condition: service_healthy
    command: npm run test:ci
    volumes:
      - ../../spec-sdk-tests/test-results:/app/spec-sdk-tests/test-results
```

Create `build/test/Dockerfile.test`:

```dockerfile
FROM node:20-alpine

WORKDIR /app

# Copy package files
COPY spec-sdk-tests/package*.json ./spec-sdk-tests/

# Install dependencies
RUN cd spec-sdk-tests && npm ci

# Copy test files
COPY spec-sdk-tests ./spec-sdk-tests

CMD ["npm", "test"]
```

## Acceptance Criteria

- [ ] GitHub Actions workflow file created and tested
- [ ] Tests run automatically on PRs and commits to main
- [ ] Test environment spins up in < 5 minutes
- [ ] All 147 tests execute successfully in CI
- [ ] Test results uploaded as artifacts
- [ ] Test summary appears in PR comments
- [ ] README badge displays current test status
- [ ] Failed tests on main branch create alerts
- [ ] Workflow supports manual triggering with destination filter
- [ ] Docker Compose setup works locally and in CI

## Dependencies

- GitHub Actions runners with Docker support
- Repository secrets configured (for external service tests)
- Docker images published for Outpost
- PostgreSQL and Redis available in CI environment

## Risks & Considerations

1. **External Service Dependencies**
   - Risk: Tests requiring AWS/Azure/GCP may be flaky or slow
   - Mitigation: Use test mode with mocks for PR tests, real services for nightly runs

2. **Test Execution Time**
   - Risk: 147 tests may take too long in CI
   - Mitigation: Run tests in parallel, set 20-minute timeout

3. **Resource Constraints**
   - Risk: GitHub Actions runners may have limited resources
   - Mitigation: Use service containers, optimize Docker images

4. **Flaky Tests**
   - Risk: Network/timing issues may cause intermittent failures
   - Mitigation: Implement retries, increase timeouts, add health checks

5. **Cost**
   - Risk: Frequent test runs may consume GitHub Actions minutes
   - Mitigation: Optimize workflow triggers, cache dependencies

## Future Enhancements

- Matrix strategy for testing multiple Go/Node versions
- Parallel test execution by destination type
- Performance benchmarking in CI
- Visual regression testing for generated SDKs
- Integration with code coverage tools (Codecov, Coveralls)

---

**Estimated Effort**: 2-3 days  
**Priority**: High  
**Dependencies**: None (ready to implement)