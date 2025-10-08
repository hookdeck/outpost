# Load Testing Overview

## Prerequisites

- k6 installed
- Docker running
- Outpost deployment with API key
- Node.js (for TypeScript compilation)

## Two-Phase Load Test

### 1. Throughput Test
Creates a tenant with one webhook destination and publishes events at a configured rate. Event IDs are stored in Redis for verification.

### 2. Verification Test
Queries the mock webhook to confirm delivery and measure latency metrics:
- End-to-end latency (publish to delivery)
- Receive latency (publish to Outpost receipt)
- Internal latency (Outpost processing time)

## Setup

### Start Supporting Services

```bash
cd loadtest
docker-compose up -d
```

This starts:
- **Redis** (`localhost:46379`): Coordinates test state between throughput and verification phases
- **Mock Webhook** (`localhost:48080`): Receives webhook deliveries and stores them for verification

### Configure Environment

Use `loadtest/config/environments/local.json` or create a new one (e.g., `staging.json`):

```json
{
  "name": "local",
  "api": {
    "baseUrl": "http://localhost:3333",
    "timeout": "30s"
  },
  "mockWebhook": {
    "url": "http://localhost:48080",
    "destinationUrl": "http://host.docker.internal:48080",
    "verificationPollTimeout": "5s"
  },
  "redis": "redis://localhost:46379"
}
```

**Critical:** `mockWebhook.destinationUrl` must be accessible from your Outpost deployment:
- **Local Outpost in Docker**: `http://host.docker.internal:48080`
- **Local Outpost in Kubernetes**: `http://host.docker.internal:48080`
- **Remote Outpost**: Expose mock webhook publicly (e.g., ngrok tunnel) and use that URL

The mock webhook must be reachable by Outpost for event delivery to succeed.

### Configure Scenario

Use the default `basic.json` scenario, edit it locally, or create a new one.

Default scenario at `loadtest/config/scenarios/events-throughput/basic.json`:

```json
{
  "options": {
    "scenarios": {
      "events": {
        "rate": 100,
        "timeUnit": "1s",
        "duration": "30s",
        "preAllocatedVUs": 20
      }
    }
  }
}
```

To create a new scenario, add a file (e.g., `high-load.json`) in the same directory and reference it with `--scenario high-load`.

## Running Tests

### Throughput Test

```bash
export API_KEY=your-api-key
export TESTID=$(date +%s)

./run-test.sh events-throughput --environment local --scenario basic
```

### Verification Test

```bash
# Use same TESTID from throughput test
# MAX_ITERATIONS = rate × duration (e.g., 100 × 30 = 3000)
MAX_ITERATIONS=3000 ./run-test.sh events-verify --environment local --scenario basic
```

## Mock Webhook

The mock webhook service provides:
- `POST /webhook`: Receives event deliveries from Outpost
- `GET /events/{eventId}`: Returns event details for verification
- `GET /health`: Service status

Events are stored in an LRU cache with 10-minute expiration.

**Network Requirements:**
- k6 must reach mock webhook at `mockWebhook.url` to verify deliveries
- Outpost must reach mock webhook at `mockWebhook.destinationUrl` to deliver events
- For remote Outpost deployments, expose the mock webhook via tunnel or public endpoint

## Cleanup

```bash
docker-compose down
```
